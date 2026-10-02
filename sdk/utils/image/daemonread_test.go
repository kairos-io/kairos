package image_test

// This file reproduces kairos-io/kairos#4946 against whatever Docker engine is
// installed on the machine running it, and it is the only part of the issue
// that can be settled by a test.
//
// The report is that a `docker-daemon:` image reads back as an empty tree on
// the Ubuntu 26.04 engine: the copy into the ISO root filesystem reports
// success in about three seconds and writes nothing, so hadron's arm64 ISO job
// is pinned to `ubuntu-24.04-arm`. The read is the same one a user makes when
// building an ISO from an image they built locally, so it is not only a CI
// problem.
//
// It needs a Docker daemon, so it is skipped unless
// KAIROS_DAEMON_READ_TEST is set. `.github/workflows/pr-fork-checks.yaml`
// runs it across the 24.04 and 26.04 runner images on both architectures,
// which is what dates the failure to an engine version rather than to the
// image being read.
//
// One hypothesis is already ruled out, and not by this test: the containerd
// image store's export format. Driving containerd's own exporter
// (core/images/archive.Export, with the option set moby's ExportImage passes:
// WithSkipNonDistributableBlobs, WithPlatform, WithSkipMissing, WithManifest)
// and reading the result back through go-containerregistry's daemon path
// yields the layer and its entries. An OCI-layout archive whose manifest.json
// names blobs/sha256/... paths is read correctly, so the empty tree is not the
// legacy-versus-OCI archive shape.

import (
	"archive/tar"
	"bytes"

	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/kairos-io/kairos/v4/sdk/utils/image"
)

// daemonReadEnv gates the test, because a Docker daemon is not part of the
// unit-test environment.
const daemonReadEnv = "KAIROS_DAEMON_READ_TEST"

// markerPath is a file every Kairos system image has and the ISO build reads
// first. Its absence is the failure the issue reports: "open
// /output/temp-rootfs/etc/os-release: no such file or directory".
const markerPath = "etc/os-release"

// markerBody is written into the fixture image so a successful read can be
// told apart from a read that produced the right names and no content.
const markerBody = "ID=kairos\nKAIROS_DAEMON_READ_FIXTURE=1\n"

func requireDaemon(t *testing.T) {
	t.Helper()
	if os.Getenv(daemonReadEnv) == "" {
		t.Skipf("set %s=1 to run this against the local Docker daemon", daemonReadEnv)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("no docker binary on PATH: %v", err)
	}
	if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("no reachable Docker daemon: %v: %s", err, out)
	} else {
		t.Logf("docker server version: %s", strings.TrimSpace(string(out)))
	}
}

// buildFixtureImage builds a scratch image holding nothing but the marker file
// and returns its tag. `FROM scratch` keeps the build offline: a runner with no
// registry access, or a rate-limited one, must not turn this into a flake.
func buildFixtureImage(t *testing.T, tag string) {
	t.Helper()

	ctx := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ctx, filepath.Dir(markerPath)), 0755); err != nil {
		t.Fatalf("preparing the build context: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctx, markerPath), []byte(markerBody), 0644); err != nil {
		t.Fatalf("writing %s: %v", markerPath, err)
	}
	dockerfile := fmt.Sprintf("FROM scratch\nCOPY %s /%s\n", markerPath, markerPath)
	if err := os.WriteFile(filepath.Join(ctx, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatalf("writing the Dockerfile: %v", err)
	}

	cmd := exec.Command("docker", "build", "--tag", tag, ".")
	cmd.Dir = ctx
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "image", "rm", "--force", tag).Run()
	})
}

// extractedNames reads the tar stream ExtractOCIImage applies and returns the
// entry names and the marker's contents.
//
// It stops short of ExtractOCIImage itself on purpose. That function chowns
// each entry to the uid and gid the layer records, which is 0 here and fails
// for the unprivileged user a hosted runner runs as. The stream it applies is
// the tree the issue reports as empty, and its own emptiness check counts
// exactly these entries, so reading the stream measures the same thing without
// needing root.
func extractedNames(t *testing.T, img v1.Image) ([]string, string) {
	t.Helper()

	reader := mutate.Extract(img)
	defer reader.Close()

	var names []string
	var marker string
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading the extraction stream after %d entries: %v", len(names), err)
		}
		names = append(names, header.Name)
		if strings.TrimPrefix(header.Name, "./") == markerPath {
			body, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("reading %s out of the extraction stream: %v", markerPath, err)
			}
			marker = string(body)
		}
	}
	return names, marker
}

// TestDockerDaemonImageReadsBackItsContents is kairos-io/kairos#4946. An image
// that exists only in the local daemon has to read back through GetImage with
// the layers it was built with. When it does not, every caller that copies a
// root filesystem out of a locally built image gets an empty directory: the
// ISO build then fails three steps later reporting a missing kernel, and the
// message points at the kernel rather than at the read.
func TestDockerDaemonImageReadsBackItsContents(t *testing.T) {
	requireDaemon(t)

	tag := "kairos.test/daemonread:4946"
	buildFixtureImage(t, tag)

	img, err := image.GetImage(tag, "", nil, nil)
	if err != nil {
		t.Fatalf("GetImage(%s): %v", tag, err)
	}

	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("reading the layers of %s: %v", tag, err)
	}
	if len(layers) == 0 {
		t.Fatalf("%s reports zero layers; it was built with one", tag)
	}

	names, marker := extractedNames(t, img)
	if len(names) == 0 {
		t.Fatalf("%s unpacked to an empty tree: this is kairos-io/kairos#4946 reproducing on this engine", tag)
	}
	t.Logf("extracted %d entries: %v", len(names), names)

	if marker != markerBody {
		t.Errorf("%s came back as %q, want %q", markerPath, marker, markerBody)
	}
}

// TestDockerDaemonLoadedImageReadsBackItsContents covers the shape hadron's
// arm64 lane actually uses. Since kairos-io/hadron#566 the image is handed
// between jobs as a `docker save` tarball, so the daemon that AuroraBoot reads
// from received the image through `docker load` rather than by building it. A
// store that re-exports a loaded archive differently from one it built itself
// would show up here and not in the test above.
func TestDockerDaemonLoadedImageReadsBackItsContents(t *testing.T) {
	requireDaemon(t)

	built := "kairos.test/daemonread-source:4946"
	buildFixtureImage(t, built)

	archivePath := filepath.Join(t.TempDir(), "save.tar")
	save := exec.Command("docker", "save", "--output", archivePath, built)
	if out, err := save.CombinedOutput(); err != nil {
		t.Fatalf("docker save: %v\n%s", err, out)
	}

	loaded := "kairos.test/daemonread-loaded:4946"
	if out, err := exec.Command("docker", "image", "rm", "--force", built).CombinedOutput(); err != nil {
		t.Fatalf("removing the built image before the load: %v\n%s", err, out)
	}
	if out, err := exec.Command("docker", "load", "--input", archivePath).CombinedOutput(); err != nil {
		t.Fatalf("docker load: %v\n%s", err, out)
	}
	if out, err := exec.Command("docker", "tag", built, loaded).CombinedOutput(); err != nil {
		t.Fatalf("docker tag: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "image", "rm", "--force", loaded).Run()
	})

	img, err := image.GetImage(loaded, "", nil, nil)
	if err != nil {
		t.Fatalf("GetImage(%s): %v", loaded, err)
	}

	names, marker := extractedNames(t, img)
	if len(names) == 0 {
		t.Fatalf("%s unpacked to an empty tree after a save and load round trip: this is kairos-io/kairos#4946 reproducing on this engine", loaded)
	}
	t.Logf("extracted %d entries: %v", len(names), names)

	if marker != markerBody {
		t.Errorf("%s came back as %q, want %q", markerPath, marker, markerBody)
	}
}

// TestExtractedNamesTellsAnEmptyTreeFromAFullOne proves the reproduction above
// can actually fail. A test that reads a tar stream and asserts on what it
// finds is only worth running if it reports nothing for an image with no
// layers and the marker for an image that carries it, so both are driven here
// with no daemon involved.
func TestExtractedNamesTellsAnEmptyTreeFromAFullOne(t *testing.T) {
	names, marker := extractedNames(t, empty.Image)
	if len(names) != 0 {
		t.Errorf("an image with no layers extracted %d entries: %v", len(names), names)
	}
	if marker != "" {
		t.Errorf("an image with no layers produced %s as %q", markerPath, marker)
	}

	var layerTar bytes.Buffer
	tw := tar.NewWriter(&layerTar)
	if err := tw.WriteHeader(&tar.Header{
		Name:     markerPath,
		Mode:     0644,
		Size:     int64(len(markerBody)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("writing the fixture tar header: %v", err)
	}
	if _, err := tw.Write([]byte(markerBody)); err != nil {
		t.Fatalf("writing the fixture tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing the fixture tar: %v", err)
	}

	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(layerTar.Bytes())), nil
	})
	if err != nil {
		t.Fatalf("building the fixture layer: %v", err)
	}
	full, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("appending the fixture layer: %v", err)
	}

	names, marker = extractedNames(t, full)
	if len(names) != 1 || names[0] != markerPath {
		t.Errorf("a one-layer image extracted %v, want exactly [%s]", names, markerPath)
	}
	if marker != markerBody {
		t.Errorf("%s came back as %q, want %q", markerPath, marker, markerBody)
	}
}
