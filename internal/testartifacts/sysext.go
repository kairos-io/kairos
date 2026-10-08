package testartifacts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The extensions built here carry /usr/bin/hello.sh, which prints "Hello
// world". /usr/bin is a hierarchy Kairos merges (/usr/local is not, it is the
// persistent partition), so a merged extension puts the script on the host's
// PATH, and the end-to-end suite runs it inside the VM.
const helloScript = "#!/bin/sh\necho \"Hello world\"\n"

// SysextOptions describes a system extension image to build.
type SysextOptions struct {
	// Dir is where <Name>.sysext.raw is written.
	Dir string
	// Name is the extension name.
	Name string
	// Arch is amd64 or arm64.
	Arch string
	// KeyFile and CertFile sign the verity root hash. Leave both empty for
	// a verity-only image; setting one without the other is an error.
	KeyFile, CertFile string
}

// BuildSysext builds a systemd-repart DDI system extension with AuroraBoot
// sysext: an erofs payload holding hello.sh, a verity hash partition and,
// when a key and certificate are given, a verity signature partition.
func BuildSysext(ctx context.Context, opts SysextOptions) (string, error) {
	if (opts.KeyFile == "") != (opts.CertFile == "") {
		return "", errors.New("a signed extension needs both KeyFile and CertFile")
	}
	for _, p := range []*string{&opts.Dir, &opts.KeyFile, &opts.CertFile} {
		abs, err := absIfSet(*p)
		if err != nil {
			return "", err
		}
		*p = abs
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return "", err
	}

	src, err := os.MkdirTemp("", "testartifacts-sysext-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(src)
	if err := os.WriteFile(filepath.Join(src, "hello.sh"), []byte(helloScript), 0o755); err != nil {
		return "", err
	}
	dockerfile := "FROM scratch\nCOPY hello.sh /usr/bin/hello.sh\n"
	if err := os.WriteFile(filepath.Join(src, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", err
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	tag := "kairos-testartifacts-sysext:" + hex.EncodeToString(suffix)
	if err := docker(ctx, "build", "-q", "--platform", "linux/"+opts.Arch, "-t", tag, src); err != nil {
		return "", err
	}
	defer func() { _ = docker(context.Background(), "rmi", "-f", tag) }()

	sock, err := socketArgs()
	if err != nil {
		return "", err
	}
	args := append([]string{"run", "--rm"}, userArgs()...)
	args = append(args, sock...)
	args = append(args, "-v", opts.Dir+":/out")
	sysextArgs := []string{"sysext", "--arch", opts.Arch, "--output", "/out"}
	if opts.KeyFile != "" {
		args = append(args, "-v", opts.KeyFile+":/keys/key.pem:ro", "-v", opts.CertFile+":/keys/cert.pem:ro")
		sysextArgs = append(sysextArgs, "--private-key", "/keys/key.pem", "--certificate", "/keys/cert.pem")
	}
	args = append(args, AuroraBootImage)
	args = append(args, sysextArgs...)
	args = append(args, opts.Name, tag)
	if err := docker(ctx, args...); err != nil {
		return "", err
	}
	return filepath.Join(opts.Dir, opts.Name+".sysext.raw"), nil
}

// BuildPlainSquashfsSysext builds a bare squashfs extension holding hello.sh,
// with no verity and no signature, using the mksquashfs that ships in the
// AuroraBoot image. A trusted boot image policy rejects it, which is what it
// is for.
func BuildPlainSquashfsSysext(ctx context.Context, dir, name string) (string, error) {
	dir, err := absIfSet(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src, err := os.MkdirTemp("", "testartifacts-squashfs-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(src)
	if err := os.MkdirAll(filepath.Join(src, "usr/bin"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(src, "usr/bin/hello.sh"), []byte(helloScript), 0o755); err != nil {
		return "", err
	}
	releaseDir := filepath.Join(src, "usr/lib/extension-release.d")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(releaseDir, "extension-release."+name), []byte("ID=_any\n"), 0o644); err != nil {
		return "", err
	}

	out := name + ".sysext.raw"
	args := append([]string{"run", "--rm"}, userArgs()...)
	args = append(args, "-v", src+":/src:ro", "-v", dir+":/out", "--entrypoint", "mksquashfs",
		AuroraBootImage, "/src", "/out/"+out, "-all-root", "-noappend", "-quiet")
	if err := docker(ctx, args...); err != nil {
		return "", fmt.Errorf("building %s: %w", out, err)
	}
	return filepath.Join(dir, out), nil
}

// absIfSet makes a non-empty path absolute. docker run -v treats a relative
// source as a named volume, so every path handed to it must be absolute.
func absIfSet(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return filepath.Abs(path)
}
