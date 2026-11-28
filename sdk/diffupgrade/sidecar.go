package diffupgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// SidecarSuffix is appended to the path of a state image to name the file
// that records its RootfsID: active.img has its ID in active.img.id.
//
// A deployed image cannot be measured again to learn which release it holds:
// hooks, SELinux relabeling and the bootloader setup change it as part of the
// install or upgrade that wrote it. So its ID is taken while it is still the
// release, right after it is unpacked, and kept next to it.
const SidecarSuffix = ".id"

// ErrNoRecordedID is returned by RecordedID when no ID can be trusted for an
// image.
var ErrNoRecordedID = errors.New("no rootfs ID recorded for this image")

// sidecar is what the file holds. The image's size and modification time pin
// the ID to the image as it was when the ID was recorded. Anything that
// rewrites the image without knowing about the sidecar, such as the agent of
// an older recovery system resetting the node, changes them and so voids the
// ID instead of leaving it to name a release the image no longer holds.
type sidecar struct {
	RootfsID string    `json:"rootfs-id"`
	Size     int64     `json:"image-size"`
	ModTime  time.Time `json:"image-mtime"`
}

// sidecarFS is the part of a filesystem the sidecar functions need.
type sidecarFS interface {
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Remove(name string) error
}

// RecordID records id as the RootfsID of the image at imgFile, as the image
// is now. Call it once nothing else is going to write the image.
func RecordID(fsys sidecarFS, imgFile, id string) error {
	info, err := fsys.Stat(imgFile)
	if err != nil {
		return err
	}
	data, err := json.Marshal(sidecar{RootfsID: id, Size: info.Size(), ModTime: info.ModTime()})
	if err != nil {
		return err
	}
	return fsys.WriteFile(imgFile+SidecarSuffix, data, 0o600)
}

// RecordedID returns the RootfsID recorded for the image at imgFile. It
// returns an error wrapping ErrNoRecordedID when there is none, or when the
// image has been rewritten since it was recorded.
func RecordedID(fsys sidecarFS, imgFile string) (string, error) {
	data, err := fsys.ReadFile(imgFile + SidecarSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s has none", ErrNoRecordedID, imgFile)
	}
	if err != nil {
		return "", err
	}
	var s sidecar
	if err := json.Unmarshal(data, &s); err != nil || s.RootfsID == "" {
		return "", fmt.Errorf("%w: %s%s cannot be read", ErrNoRecordedID, imgFile, SidecarSuffix)
	}
	info, err := fsys.Stat(imgFile)
	if err != nil {
		return "", err
	}
	if info.Size() != s.Size || !info.ModTime().Equal(s.ModTime) {
		return "", fmt.Errorf("%w: %s was rewritten after its ID was recorded", ErrNoRecordedID, imgFile)
	}
	return s.RootfsID, nil
}

// ForgetID removes the ID recorded for the image at imgFile, if any.
func ForgetID(fsys sidecarFS, imgFile string) error {
	if err := fsys.Remove(imgFile + SidecarSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
