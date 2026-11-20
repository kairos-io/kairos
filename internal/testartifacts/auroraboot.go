package testartifacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	// binaryEnv names an environment variable holding the path of an AuroraBoot
	// binary to use as is, skipping the build.
	binaryEnv = "KAIROS_TEST_AURORABOOT_BINARY"

	// imageEnv names an environment variable holding an AuroraBoot container
	// image to use as is, skipping the build. CI sets it to the image it built
	// from the commit under test.
	imageEnv = "KAIROS_TEST_AURORABOOT_IMAGE"

	// localImage is the tag the image built from the source tree gets.
	localImage = "kairos-testartifacts-auroraboot:local"
)

// cacheDir returns the directory built binaries are kept in.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "kairos", "testartifacts"), nil
}

// sourceDir returns the auroraboot module of the checkout this package comes
// from. AuroraBoot lives in the same repository, so the helpers always run the
// AuroraBoot of the commit under test.
func sourceDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("cannot locate the AuroraBoot source")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "auroraboot")
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return "", fmt.Errorf("AuroraBoot source not found at %s: %w; set %s or %s", dir, err, binaryEnv, imageEnv)
	}
	return filepath.Clean(dir), nil
}

// AuroraBootBinary returns the path of an AuroraBoot binary built from the
// source tree, which it builds into a per-user cache. It needs a Go toolchain,
// a C compiler and libpcsclite. KAIROS_TEST_AURORABOOT_BINARY, when set, names
// a binary that is used as is.
//
// The binary is written to a temporary file and renamed into place, so
// concurrent callers never see a partial file. Rebuilding on every call is
// cheap, because the Go build cache keeps the compiled packages.
func AuroraBootBinary(ctx context.Context) (string, error) {
	if path := os.Getenv(binaryEnv); path != "" {
		return path, nil
	}
	src, err := sourceDir()
	if err != nil {
		return "", err
	}
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "auroraboot-*")
	if err != nil {
		return "", err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	cmd := exec.CommandContext(ctx, "go", "build", "-o", tmp.Name(), ".")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building auroraboot: %w\n%s", err, out)
	}
	final := filepath.Join(dir, "auroraboot")
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

// AuroraBootImage returns the AuroraBoot container image the Docker helpers
// run, which it builds from the source tree. KAIROS_TEST_AURORABOOT_IMAGE, when
// set, names an image that is used as is. The build hands the repository root
// to the Dockerfile as the named context "kairos", because the auroraboot
// module replaces the root module with ../.
func AuroraBootImage(ctx context.Context) (string, error) {
	if image := os.Getenv(imageEnv); image != "" {
		return image, nil
	}
	src, err := sourceDir()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(src)
	err = docker(ctx, "buildx", "build", "--load", "-q", "-t", localImage,
		"--build-context", "kairos="+root, "-f", filepath.Join(src, "Dockerfile"), src)
	if err != nil {
		return "", err
	}
	return localImage, nil
}

// GenerateKeySet writes a throwaway Secure Boot key set (PK, KEK and db as
// .key, .pem, .der, .esl and .auth) and the TPM PCR policy signing key
// tpm2-pcr-private.pem into dir, using AuroraBoot genkey from the release
// binary. It needs openssl and libpcsclite on the host, not Docker. The
// certificates expire after 30 days.
func GenerateKeySet(ctx context.Context, dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	bin, err := AuroraBootBinary(ctx)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "genkey", "-e", "30", "-o", dir, "kairos-test")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("auroraboot genkey: %w\n%s", err, out)
	}
	return nil
}
