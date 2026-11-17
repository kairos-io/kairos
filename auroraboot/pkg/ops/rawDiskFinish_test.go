package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
)

// newFinishTestImage returns a RawImage whose source reports the given board
// model, and an empty image file for it to finish.
func newFinishTestImage(t *testing.T, model string) (*RawImage, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(source, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	release := "KAIROS_FLAVOR=\"ubuntu\"\nKAIROS_MODEL=\"" + model + "\"\n"
	if err := os.WriteFile(filepath.Join(source, "etc", "kairos-release"), []byte(release), 0o644); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(image, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return &RawImage{
		config: config.NewConfig(config.WithLogger(internal.Log)),
		Source: source,
	}, image
}

func TestFinishDiskImageFailsWhenTheBoardBootloaderIsMissing(t *testing.T) {
	if _, err := os.Stat("/arm/pinebookpro/idbloader.img"); err == nil {
		t.Skip("pinebookpro bootloader blobs are present on this host")
	}
	r, image := newFinishTestImage(t, "pinebookpro")

	err := r.finishDiskImage(image)
	if err == nil {
		t.Fatal("expected an error when the pinebookpro bootloader blob is missing, got nil")
	}
	if !strings.Contains(err.Error(), "idbloader.img") {
		t.Fatalf("expected the error to name the missing blob, got: %v", err)
	}
}

func TestFinishDiskImageGrowsTheImageToTheFinalSize(t *testing.T) {
	r, image := newFinishTestImage(t, "generic")
	r.FinalSize = 2

	if err := r.finishDiskImage(image); err != nil {
		t.Fatalf("finishing a generic image: %v", err)
	}
	info, err := os.Stat(image)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 2*1024*1024 {
		t.Fatalf("expected the image to be 2MiB, got %d bytes", info.Size())
	}
}
