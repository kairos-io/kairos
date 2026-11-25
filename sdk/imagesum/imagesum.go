// Package imagesum records and checks a sha256 checksum next to each OS image
// (active.img, passive.img, recovery.img) on the state and recovery partitions.
//
// kairos-agent writes the checksums when it installs, upgrades or resets, and
// immucore checks them at boot before it loop-mounts the image. Both use this
// package, so the on-disk format has one implementation.
//
// The checksum lives in a sidecar file named "<image>.sha256" in the format
// understood by sha256sum(1): "<hex digest>  <image basename>\n". A second,
// comment line records the size and mtime of the image when it was hashed:
// "# size=<bytes> mtime_ns=<unix ns>\n". If either changed, the image was
// replaced by something that did not update the sidecar (for example an agent
// that predates this feature), so the checksum is stale and is not used. The
// same size and mtime with a different digest is real corruption.
//
// Invariant: a sidecar that exists is correct for the image next to it. Code that
// replaces or modifies an image must Remove the sidecar first and write a new one
// after. A missing sidecar means "not checked" and is always safe, which is what
// images created by older agents look like.
//
// This protects against bit rot, truncated writes and accidental modification.
// It does not protect against an attacker with write access to the partition,
// who can rewrite the sidecar as well.
package imagesum

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkFs "github.com/kairos-io/kairos/v4/sdk/types/fs"
)

const (
	// Suffix is appended to the image path to get the sidecar path.
	Suffix = ".sha256"
	// hashBufferSize is the read size when hashing. Images are GBs, big reads
	// keep the syscall count low.
	hashBufferSize = 1 << 20
)

// Applies reports whether path is an image file we checksum. Squashfs recovery
// images are not loop-mounted by immucore, so they are left alone.
func Applies(path string) bool {
	return strings.HasSuffix(path, ".img")
}

// SidecarPath returns the checksum file path for an image.
func SidecarPath(img string) string { return img + Suffix }

// Compute streams the file through sha256 and returns the hex digest.
func Compute(fs sdkFs.KairosFS, path string) (string, error) {
	sum, _, err := computeContext(context.Background(), fs, path)
	return sum, err
}

// ErrBudgetExceeded is returned (wrapped) when hashing is stopped because the
// context ended before the whole image was read.
var ErrBudgetExceeded = errors.New("time budget exceeded")

// computeContext is Compute that stops between two reads once ctx is done. It
// returns the number of bytes hashed so far either way. A read already in
// flight is not interrupted, so it overshoots by at most one buffer.
func computeContext(ctx context.Context, fs sdkFs.KairosFS, path string) (string, int64, error) {
	f, err := fs.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, hashBufferSize)
	var n int64
	for {
		if ctx.Err() != nil {
			return "", n, fmt.Errorf("hashing %s: %w", path, ErrBudgetExceeded)
		}
		r, rerr := f.Read(buf)
		if r > 0 {
			h.Write(buf[:r])
			n += int64(r)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", n, fmt.Errorf("hashing %s: %w", path, rerr)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Write stores sum as the sidecar of img, with the current size and mtime of img.
// The write is atomic: the sidecar is either absent/old or complete, never partial.
func Write(fs sdkFs.KairosFS, img, sum string) error {
	info, err := fs.Stat(img)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("%s  %s\n# size=%d mtime_ns=%d\n", sum, filepath.Base(img), info.Size(), info.ModTime().UnixNano())
	return writeAtomic(fs, SidecarPath(img), []byte(line))
}

// writeAtomic writes data to a temp file next to path, syncs it and renames it in place.
func writeAtomic(fs sdkFs.KairosFS, path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := fs.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = fs.Remove(tmp)
		return err
	}
	if err = fs.Rename(tmp, path); err != nil {
		_ = fs.Remove(tmp)
		return err
	}
	return nil
}

// Generate hashes img and writes its sidecar. It returns the digest.
func Generate(fs sdkFs.KairosFS, img string) (string, error) {
	sum, err := Compute(fs, img)
	if err != nil {
		return "", err
	}
	return sum, Write(fs, img, sum)
}

// Remove deletes the sidecar of img, ignoring a missing one.
func Remove(fs sdkFs.KairosFS, img string) error {
	err := fs.Remove(SidecarPath(img))
	if err != nil && !isNotExist(err) {
		return err
	}
	return nil
}

// Sum is the content of a sidecar file.
type Sum struct {
	Digest string
	// HasMeta is true if the file records the size and mtime of the image at hash time.
	HasMeta bool
	Size    int64
	MtimeNs int64
}

// Read parses the sidecar of img. found is false when there is no sidecar. A
// sidecar that cannot be parsed is an error.
func Read(fs sdkFs.KairosFS, img string) (sum Sum, found bool, err error) {
	path := SidecarPath(img)
	data, err := fs.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return Sum{}, false, nil
		}
		return Sum{}, false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			var size, mtime int64
			if n, _ := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "#")), "size=%d mtime_ns=%d", &size, &mtime); n == 2 {
				sum.HasMeta, sum.Size, sum.MtimeNs = true, size, mtime
			}
		case sum.Digest == "":
			digest := strings.Fields(line)[0]
			if len(digest) != sha256.Size*2 {
				return Sum{}, true, fmt.Errorf("malformed checksum file %s: %q is not a sha256 digest", path, digest)
			}
			if _, err := hex.DecodeString(digest); err != nil {
				return Sum{}, true, fmt.Errorf("malformed checksum file %s: %w", path, err)
			}
			sum.Digest = strings.ToLower(digest)
		}
	}
	if sum.Digest == "" {
		return Sum{}, true, fmt.Errorf("malformed checksum file %s: no digest found", path)
	}
	return sum, true, nil
}

// Result is the outcome of checking an image.
type Result int

const (
	// Missing means there is no checksum file, the image was not checked. This is
	// the state of every image written by an agent that predates the feature.
	Missing Result = iota
	// OK means the image matches its checksum.
	OK
	// Mismatch means the image differs from its checksum although its size and
	// mtime are what they were when it was hashed: corruption.
	Mismatch
	// Stale means the image has a different size or mtime than when it was
	// hashed, so it was replaced or modified without the checksum being updated
	// (e.g. by an older agent). The checksum says nothing about this image and the
	// image was not read.
	Stale
	// TimedOut means hashing was stopped by the time budget before the whole
	// image was read, so nothing is known about its content. Report.BytesHashed
	// says how far it got. It only comes from CheckWithin.
	TimedOut
)

func (r Result) String() string {
	switch r {
	case Missing:
		return "missing"
	case OK:
		return "ok"
	case Mismatch:
		return "mismatch"
	case Stale:
		return "stale"
	case TimedOut:
		return "timed out"
	}
	return "unknown"
}

// Report is what Check found out about an image.
type Report struct {
	Result Result
	// Expected is the digest stored in the sidecar, empty if there is none.
	Expected string
	// Actual is the digest computed from the image, empty if the image was not read.
	Actual string
	// BytesHashed is how much of the image was read before a TimedOut result.
	BytesHashed int64
}

// Check compares img with its sidecar. With no sidecar, or a stale one, the
// image is not read. A sidecar that cannot be parsed or an image that cannot be
// read is an error, not a Mismatch.
func Check(fs sdkFs.KairosFS, img string) (Report, error) {
	return CheckContext(context.Background(), fs, img)
}

// CheckWithin is Check with a time budget for reading the image. When it runs
// out the hashing stops and the result is TimedOut, not an error: the image is
// neither good nor bad, it was not checked. A budget of zero or less means no
// limit.
func CheckWithin(fs sdkFs.KairosFS, img string, budget time.Duration) (Report, error) {
	if budget <= 0 {
		return Check(fs, img)
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	return CheckContext(ctx, fs, img)
}

// CheckContext is Check that stops reading the image when ctx is done and then
// reports TimedOut.
func CheckContext(ctx context.Context, fs sdkFs.KairosFS, img string) (Report, error) {
	sum, found, err := Read(fs, img)
	if err != nil {
		return Report{}, err
	}
	if !found {
		return Report{Result: Missing}, nil
	}
	info, err := fs.Stat(img)
	if err != nil {
		return Report{Expected: sum.Digest}, err
	}
	if sum.HasMeta && (info.Size() != sum.Size || info.ModTime().UnixNano() != sum.MtimeNs) {
		return Report{Result: Stale, Expected: sum.Digest}, nil
	}
	got, n, err := computeContext(ctx, fs, img)
	if errors.Is(err, ErrBudgetExceeded) {
		return Report{Result: TimedOut, Expected: sum.Digest, BytesHashed: n}, nil
	}
	if err != nil {
		return Report{Expected: sum.Digest}, err
	}
	if got != sum.Digest {
		return Report{Result: Mismatch, Expected: sum.Digest, Actual: got}, nil
	}
	return Report{Result: OK, Expected: sum.Digest, Actual: got}, nil
}

// isNotExist also matches the fs.ErrNotExist wrapped by vfs implementations.
func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
