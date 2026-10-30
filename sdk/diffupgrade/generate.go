package diffupgrade

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Root is one side of a diff: a root filesystem unpacked at Dir, and the
// name of the image it came from, recorded in the manifest.
type Root struct {
	Dir string
	Ref string
}

// Generate writes to w the layer of the diff that turns the root filesystem
// at base into the one at target, and returns its manifest.
//
// A path is carried when it is new in target or differs from base in type,
// permissions, owner, extended attributes, link target, device number or
// contents. Modification times alone do not count: every build stamps fresh
// ones, and a diff that carried them would carry the whole image. A file that
// only changed its time therefore keeps the time it had on the base.
//
// Files that share an inode in target are written as one file and hard
// links to it, as far as the diff carries them: a hard link to a file the
// diff does not carry becomes a copy. Sockets are skipped, as a root
// filesystem image has no use for them.
func Generate(base, target Root, w io.Writer) (*Manifest, error) {
	baseRelease, err := Fingerprint(osFS{}, base.Dir)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	targetRelease, err := Fingerprint(osFS{}, target.Dir)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	baseID, err := RootfsID(base.Dir)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	targetID, err := RootfsID(target.Dir)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}

	deleted, err := deletedPaths(base.Dir, target.Dir)
	if err != nil {
		return nil, err
	}
	m := &Manifest{
		Version:       FormatVersion,
		Base:          baseID,
		Target:        targetID,
		BaseRelease:   baseRelease,
		TargetRelease: targetRelease,
		BaseRef:       base.Ref,
		TargetRef:     target.Ref,
		Deleted:       deleted,
	}

	tw := tar.NewWriter(w)
	links := map[inodeKey]string{}
	err = filepath.WalkDir(target.Dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(target.Dir, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == ManifestDir {
			return fs.SkipDir
		}
		changed, err := differs(filepath.Join(base.Dir, rel), path)
		if err != nil || !changed {
			return err
		}
		return writeEntry(tw, path, rel, links)
	})
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(target.Dir)
	if err != nil {
		return nil, err
	}
	rootHdr, err := header(target.Dir, rootInfo)
	if err != nil {
		return nil, err
	}
	if err := writeManifest(tw, m, rootHdr.Uid, rootHdr.Gid); err != nil {
		return nil, err
	}
	return m, tw.Close()
}

// NewImage builds the diff between base and target as a single layer image,
// ready to be written to an archive or pushed. The layer is staged in a file
// under workDir, which the image reads lazily, so release has to be called
// only once the image has been written out.
func NewImage(base, target Root, workDir string) (img v1.Image, m *Manifest, release func(), err error) {
	layerFile, err := os.CreateTemp(workDir, ".diff-layer-*.tar")
	if err != nil {
		return nil, nil, nil, err
	}
	release = func() { _ = os.Remove(layerFile.Name()) }
	defer func() {
		if err != nil {
			release()
		}
	}()

	m, err = Generate(base, target, layerFile)
	if closeErr := layerFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, nil, nil, err
	}

	layer, err := tarball.LayerFromFile(layerFile.Name())
	if err != nil {
		return nil, nil, nil, err
	}
	img, err = mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, nil, nil, err
	}
	cfg = cfg.DeepCopy()
	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	cfg.Config.Labels[BaseLabel] = m.Base
	cfg.Config.Labels[TargetLabel] = m.Target
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	return img, m, release, nil
}

// deletedPaths lists the paths base has and target does not. Below a deleted
// directory nothing more is listed. Below a directory that target turned into
// something else nothing is listed either, as unpacking the new entry already
// replaces the directory and everything in it.
func deletedPaths(base, target string) ([]string, error) {
	var deleted []string
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == "." {
			return err
		}
		// ENOTDIR means a parent of the path is no longer a directory in
		// target, which for this walk is the same as the path being gone.
		if _, err := os.Lstat(filepath.Join(target, rel)); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return err
		}
		parent, err := os.Lstat(filepath.Join(target, filepath.Dir(rel)))
		if err == nil && parent.IsDir() {
			deleted = append(deleted, rel)
		}
		if d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	sort.Strings(deleted)
	return deleted, err
}

// differs reports whether the entry at target has to be carried by the diff.
func differs(base, target string) (bool, error) {
	b, err := os.Lstat(base)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	t, err := os.Lstat(target)
	if err != nil {
		return false, err
	}

	bh, err := header(base, b)
	if err != nil {
		return false, err
	}
	th, err := header(target, t)
	if err != nil {
		return false, err
	}
	if bh.Typeflag != th.Typeflag || bh.Mode != th.Mode || bh.Uid != th.Uid || bh.Gid != th.Gid ||
		bh.Linkname != th.Linkname || bh.Devmajor != th.Devmajor || bh.Devminor != th.Devminor {
		return true, nil
	}
	bx, err := xattrs(base)
	if err != nil {
		return false, err
	}
	tx, err := xattrs(target)
	if err != nil {
		return false, err
	}
	if !sameXattrs(bx, tx) {
		return true, nil
	}
	if th.Typeflag != tar.TypeReg {
		return false, nil
	}
	if bh.Size != th.Size {
		return true, nil
	}
	same, err := sameContent(base, target)
	return !same, err
}

// header builds the tar header for path, without the name. Sockets get no
// header and are reported as an unsupported type by archive/tar.
func header(path string, info fs.FileInfo) (*tar.Header, error) {
	link := ""
	if info.Mode()&fs.ModeSymlink != 0 {
		var err error
		if link, err = os.Readlink(path); err != nil {
			return nil, err
		}
	}
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Names are looked up from the host's passwd and group files, which say
	// nothing about the image. The ids are what the agent restores.
	hdr.Uname, hdr.Gname = "", ""
	return hdr, nil
}

// inodeKey identifies a file across its hard links.
type inodeKey struct {
	dev, ino uint64
}

// writeEntry writes the entry at path to tw, named rel. links maps the inode
// of every regular file written so far that has more names to the first of
// them, so the others are written as hard links to it.
func writeEntry(tw *tar.Writer, path, rel string, links map[inodeKey]string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSocket != 0 {
		return nil
	}
	hdr, err := header(path, info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(rel)
	if info.IsDir() {
		hdr.Name += "/"
	}
	if key, ok := inode(info); ok && info.Mode().IsRegular() {
		if first, seen := links[key]; seen {
			hdr.Typeflag = tar.TypeLink
			hdr.Linkname = first
			hdr.Size = 0
			return tw.WriteHeader(hdr)
		}
		links[key] = hdr.Name
	}
	x, err := xattrs(path)
	if err != nil {
		return err
	}
	if len(x) > 0 {
		hdr.Format = tar.FormatPAX
		hdr.PAXRecords = map[string]string{}
		for k, v := range x {
			hdr.PAXRecords["SCHILY.xattr."+k] = v
		}
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if hdr.Typeflag != tar.TypeReg {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(tw, f)
	return err
}

// writeManifest writes the manifest owned like the root of the target, which
// in an image is root.
func writeManifest(tw *tar.Writer, m *Manifest, uid, gid int) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	now := time.Now()
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: ManifestDir + "/", Mode: 0o755, Uid: uid, Gid: gid, ModTime: now}); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: ManifestPath, Mode: 0o644, Uid: uid, Gid: gid, Size: int64(len(data)), ModTime: now}); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

func sameXattrs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func sameContent(a, b string) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer func() { _ = fa.Close() }()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer func() { _ = fb.Close() }()

	bufA := make([]byte, 64*1024)
	bufB := make([]byte, 64*1024)
	for {
		na, errA := io.ReadFull(fa, bufA)
		nb, errB := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		doneA := errors.Is(errA, io.EOF) || errors.Is(errA, io.ErrUnexpectedEOF)
		doneB := errors.Is(errB, io.EOF) || errors.Is(errB, io.ErrUnexpectedEOF)
		if errA != nil && !doneA {
			return false, errA
		}
		if errB != nil && !doneB {
			return false, errB
		}
		if doneA || doneB {
			return doneA == doneB, nil
		}
	}
}

// osFS reads from the host filesystem.
type osFS struct{}

func (osFS) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
