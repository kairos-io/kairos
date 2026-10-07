package testartifacts

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// binaryEnv names an environment variable holding the path of an AuroraBoot
// binary to use as is, skipping the download.
const binaryEnv = "KAIROS_TEST_AURORABOOT_BINARY"

var (
	// auroraBootReleaseURL is the base URL release assets are fetched from, as
	// <base>/<version>/<asset>.
	auroraBootReleaseURL = "https://github.com/kairos-io/AuroraBoot/releases/download"
	// auroraBootCacheDir returns the directory downloaded binaries are cached in.
	auroraBootCacheDir = defaultCacheDir
)

func defaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "kairos", "testartifacts"), nil
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// checksumFor finds the sha256 listed for asset in a checksums.txt body of
// "<sha256>  <file name>" lines.
func checksumFor(sums []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", asset)
}

// extractBinary returns the content of the auroraboot entry of a gzip tarball.
func extractBinary(tarball []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("tarball has no auroraboot entry")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == "auroraboot" {
			return io.ReadAll(tr)
		}
	}
}

// AuroraBootBinary returns the path of the AuroraBoot release binary for
// AuroraBootVersion on this machine, downloading it into a per-user cache on
// first use. KAIROS_TEST_AURORABOOT_BINARY, when set, names a binary that is
// used as is.
//
// The tarball's sha256 is compared with the release's checksums.txt before
// anything is written to the cache, which guards against a truncated or
// altered download. The binary is written to a temporary file and renamed into
// place, so concurrent callers never see a partial file.
func AuroraBootBinary(ctx context.Context) (string, error) {
	if path := os.Getenv(binaryEnv); path != "" {
		return path, nil
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("no AuroraBoot release binary for architecture %s", arch)
	}
	cache, err := auroraBootCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, fmt.Sprintf("auroraboot-%s-linux-%s", AuroraBootVersion, arch))
	final := filepath.Join(dir, "auroraboot")
	if info, err := os.Stat(final); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
		return final, nil
	}

	asset := fmt.Sprintf("auroraboot_%s_linux_%s.tar.gz", strings.TrimPrefix(AuroraBootVersion, "v"), arch)
	base := auroraBootReleaseURL + "/" + AuroraBootVersion + "/"
	sums, err := httpGet(ctx, base+"checksums.txt")
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return "", err
	}
	tarball, err := httpGet(ctx, base+asset)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(tarball)
	if hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("checksum mismatch for %s: want %s, got %s", asset, want, hex.EncodeToString(got[:]))
	}
	bin, err := extractBinary(tarball)
	if err != nil {
		return "", fmt.Errorf("%s: %w", asset, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "auroraboot-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}
