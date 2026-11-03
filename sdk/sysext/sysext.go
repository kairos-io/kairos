package sysext

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"golang.org/x/sys/unix"
)

var ErrorImageNoLayers = errors.New("image has no layers")

const (
	whiteoutPrefix    = ".wh."
	whiteoutOpaqueDir = ".wh..wh..opq"
	paxXattrPrefix    = "SCHILY.xattr."
)

// DefaultAllowListRegex provided for easy use of defaults for confext and sysext
var DefaultAllowListRegex = regexp.MustCompile(`^usr/*|^/usr/*|^etc/*|^/etc/*`)

// ExtractFilesFromLastLayer will get an image and a destination and extract the files from the last layer in the image
// into that destination.
// It will skip anything that doesn't start with /usr or /etc as its purpose is to get the files for creating a
// sysextension or a confextension
// Accepts an allowList in form of regexp.Regexp that will match the files and allow copying
func ExtractFilesFromLastLayer(image v1.Image, dst string, log sdkLogger.KairosLogger, allowList *regexp.Regexp) error {
	layers, err := image.Layers()
	if err != nil {
		return fmt.Errorf("list image layers: %w", err)
	}
	if len(layers) == 0 {
		return ErrorImageNoLayers
	}
	return extractFilesFromLayer(layers[len(layers)-1], dst, log, allowList)
}

func extractFilesFromLayer(layer v1.Layer, dst string, log sdkLogger.KairosLogger, allowList *regexp.Regexp) error {
	layerReader, err := layer.Uncompressed()
	if err != nil {
		return fmt.Errorf("read layer: %w", err)
	}
	defer func(layerReader io.ReadCloser) {
		_ = layerReader.Close()
	}(layerReader)
	tr := tar.NewReader(layerReader)
	// Every write goes through root, which confines extraction to dst: it
	// refuses any path, "..", or symlink that resolves outside it. This
	// keeps a malformed or hand-crafted layer from writing elsewhere on the
	// build host, and creates parent directories a layer omits.
	root, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	meta := &metadataRestorer{root: root, log: log, privileged: os.Geteuid() == 0}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		header.Name = filepath.Clean(header.Name)
		if !allowList.MatchString(header.Name) {
			log.Debug("Skipping ", header.Name)
			continue
		}

		// Layer paths are relative to the image root, with or without a
		// leading slash.
		name := strings.TrimPrefix(header.Name, "/")
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%s: path is outside the destination", header.Name)
		}
		// A whiteout marks a deletion of something in a lower layer:
		// https://github.com/opencontainers/image-spec/blob/79b036d80240ae530a8de15e1d21c7ab9292c693/layer.md#whiteouts
		// Only the last layer is extracted, so there is nothing for it to
		// delete, and overlayfs does not read ".wh." names as whiteouts, so
		// writing it would leave a junk file in the extension.
		if skipWhiteout(name, header.Name, log) {
			continue
		}
		mode := header.FileInfo().Mode()

		switch header.Typeflag {
		case tar.TypeDir:
			log.Debugf("%s is a directory", header.Name)
			if err := root.MkdirAll(name, mode.Perm()); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := meta.owner(name, header, false); err != nil {
				return err
			}
			if err := meta.dirXattrs(name, header); err != nil {
				return err
			}
			if err := restoreSpecialBits(root, name, mode); err != nil {
				return err
			}
			// Creating the entries below this directory moves its mtime,
			// so it is set once the whole layer is written.
			meta.deferTimes(name, header)
		case tar.TypeReg:
			log.Debugf("%s is a file", header.Name)
			if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
			if err != nil {
				return fmt.Errorf("open: %w", err)
			}
			if _, err := io.Copy(file, tr); err != nil {
				_ = file.Close()
				return fmt.Errorf("copy: %w", err)
			}
			// Writing to a file drops its file capabilities and a chown
			// drops them along with setuid, so the owner goes first, then
			// the xattrs, then the special bits.
			if err := meta.fileOwnerAndXattrs(file, name, header); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close: %w", err)
			}
			if err := restoreSpecialBits(root, name, mode); err != nil {
				return err
			}
			if err := meta.times(name, header); err != nil {
				return err
			}
		case tar.TypeSymlink:
			log.Debugf("%s is a symlink", header.Name)
			// The target is stored as is: an absolute target resolves against
			// the merged system at runtime, and root refuses to follow it
			// out of dst during extraction.
			if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return fmt.Errorf("symlink: %w", err)
			}
			if err := meta.owner(name, header, true); err != nil {
				return err
			}
		case tar.TypeLink:
			log.Debugf("%s is a hard link to %s", header.Name, header.Linkname)
			if err := extractHardLink(root, header, name, allowList, log); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported type: %d", header.Typeflag)
		}
	}
	if err := meta.applyDirTimes(); err != nil {
		return err
	}
	if meta.skipped > 0 {
		log.Warnf("Could not restore the owner or extended attributes of %d entries in %s: not permitted by the user, the user namespace or the filesystem", meta.skipped, dst)
	}
	return nil
}

// skipWhiteout reports whether name is a whiteout, warning that it is dropped.
func skipWhiteout(name, headerName string, log sdkLogger.KairosLogger) bool {
	base := filepath.Base(name)
	if !strings.HasPrefix(base, whiteoutPrefix) {
		return false
	}
	if base == whiteoutOpaqueDir {
		log.Warnf("Skipping opaque whiteout %s: the extension cannot hide the contents of %s", headerName, filepath.Dir(name))
	} else {
		log.Warnf("Skipping whiteout %s: the extension cannot delete %s", headerName, filepath.Join(filepath.Dir(name), strings.TrimPrefix(base, whiteoutPrefix)))
	}
	return true
}

// extractHardLink creates the hard link a layer entry describes. An entry whose
// target the allow list excluded is dropped with a warning.
func extractHardLink(root *os.Root, header *tar.Header, name string, allowList *regexp.Regexp, log sdkLogger.KairosLogger) error {
	// A hard link names its target relative to the root of the
	// archive, which is the one place it differs from a symlink,
	// whose target is relative to the entry. The target is always an
	// earlier entry of the same archive. git's "make install" emits
	// one per helper in /usr/libexec/git-core, and busybox and
	// coreutils install trees do the same, so an image that carries
	// any of them is full of them.
	// Clean the target the way header.Name is cleaned above. A layer
	// written with "tar -C rootfs -c ." names every entry
	// "./usr/...", and a raw "./usr/bin/git" matches no allow list
	// that the entry it points at has already matched.
	linkname := filepath.Clean(header.Linkname)
	target := strings.TrimPrefix(linkname, "/")
	// A target that climbs out of the destination is a broken layer,
	// so it fails the extraction. The allow list is asked after that,
	// because a target it excludes is only an entry this same loop
	// skipped.
	if !filepath.IsLocal(target) {
		return fmt.Errorf("%s: hard link target is outside the destination", header.Name)
	}
	if !allowList.MatchString(linkname) {
		// The target was skipped by this same loop, so there is
		// nothing on disk to link to. The allow list decided that,
		// not the layer, so drop the entry with a message that names
		// both paths rather than failing the whole extraction.
		log.Warnf("Skipping hard link %s: its target %s is outside the extension", header.Name, header.Linkname)
		return nil
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := root.Link(target, name); err != nil {
		return fmt.Errorf("hard link: %w", err)
	}
	return nil
}

// metadataRestorer applies the owner, xattrs and times a layer entry carries
// to the file extracted from it.
//
// Without root the layer owner, a group the user is not in, and the
// security and trusted xattr namespaces cannot be set. Root can be refused
// too: inside a user namespace an id with no mapping cannot be set, without
// CAP_CHOWN or CAP_SETFCAP the owner or file capabilities cannot be set, and
// the destination filesystem may not support xattrs at all. Those refusals
// are counted and logged instead of failing the extraction, so building an
// extension in a container or as a regular user keeps working. Any other
// error is returned.
type metadataRestorer struct {
	root       *os.Root
	log        sdkLogger.KairosLogger
	privileged bool
	skipped    int
	dirTimes   []dirTimes
}

type dirTimes struct {
	name         string
	atime, mtime time.Time
}

func (m *metadataRestorer) check(op, name string, err error) error {
	if err == nil {
		return nil
	}
	if m.refused(err) {
		m.log.Debugf("%s %s: %v", op, name, err)
		m.skipped++
		return nil
	}
	return fmt.Errorf("%s %s: %w", op, name, err)
}

// refused reports whether err means the system does not allow the owner or
// xattr to be set, as opposed to the extraction itself going wrong.
func (m *metadataRestorer) refused(err error) bool {
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EINVAL) ||
		errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return true
	}
	return !m.privileged && errors.Is(err, unix.EACCES)
}

// ownerIDs returns the uid and gid to chown an entry to. Only root can give
// a file away, so a regular user keeps the uid unless the layer names its
// own, and still gets the group when it is a member of it.
func (m *metadataRestorer) ownerIDs(header *tar.Header) (int, int) {
	uid := header.Uid
	if !m.privileged && uid != os.Geteuid() {
		uid = -1
	}
	return uid, header.Gid
}

func (m *metadataRestorer) owner(name string, header *tar.Header, symlink bool) error {
	uid, gid := m.ownerIDs(header)
	chown := m.root.Chown
	if symlink {
		chown = m.root.Lchown
	}
	return m.check("chown", name, chown(name, uid, gid))
}

func (m *metadataRestorer) fileOwnerAndXattrs(file *os.File, name string, header *tar.Header) error {
	uid, gid := m.ownerIDs(header)
	if err := m.check("chown", name, file.Chown(uid, gid)); err != nil {
		return err
	}
	return m.xattrs(file, name, header)
}

func (m *metadataRestorer) dirXattrs(name string, header *tar.Header) error {
	if !hasXattrs(header) {
		return nil
	}
	dir, err := m.root.Open(name)
	if err != nil {
		return m.check("open", name, err)
	}
	defer func() {
		_ = dir.Close()
	}()
	return m.xattrs(dir, name, header)
}

func (m *metadataRestorer) xattrs(file *os.File, name string, header *tar.Header) error {
	for key, value := range header.PAXRecords {
		attr, ok := strings.CutPrefix(key, paxXattrPrefix)
		if !ok {
			continue
		}
		err := unix.Fsetxattr(int(file.Fd()), attr, []byte(value), 0)
		if err := m.check("setxattr "+attr, name, err); err != nil {
			return err
		}
	}
	return nil
}

func hasXattrs(header *tar.Header) bool {
	for key := range header.PAXRecords {
		if strings.HasPrefix(key, paxXattrPrefix) {
			return true
		}
	}
	return false
}

func entryTimes(header *tar.Header) (time.Time, time.Time) {
	atime := header.AccessTime
	if atime.IsZero() {
		atime = header.ModTime
	}
	return atime, header.ModTime
}

func (m *metadataRestorer) times(name string, header *tar.Header) error {
	atime, mtime := entryTimes(header)
	return m.check("chtimes", name, m.root.Chtimes(name, atime, mtime))
}

func (m *metadataRestorer) deferTimes(name string, header *tar.Header) {
	atime, mtime := entryTimes(header)
	m.dirTimes = append(m.dirTimes, dirTimes{name: name, atime: atime, mtime: mtime})
}

func (m *metadataRestorer) applyDirTimes() error {
	for _, d := range m.dirTimes {
		if err := m.check("chtimes", d.name, m.root.Chtimes(d.name, d.atime, d.mtime)); err != nil {
			return err
		}
	}
	return nil
}

// restoreSpecialBits reapplies the setuid, setgid and sticky bits a packaged
// binary or directory may carry. root.MkdirAll and root.OpenFile accept only
// the permission bits in their mode argument, so these are set with a
// follow-up chmod.
func restoreSpecialBits(root *os.Root, name string, mode os.FileMode) error {
	special := mode & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	if special == 0 {
		return nil
	}
	if err := root.Chmod(name, mode.Perm()|special); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	return nil
}
