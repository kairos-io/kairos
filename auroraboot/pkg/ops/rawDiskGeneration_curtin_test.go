package ops

import (
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// createCurtinLandingPartitionImage relies on a static busybox being present in
// the auroraboot runtime image (dnf-installed at /usr/sbin/busybox). When it is
// missing the build must fail with a clear error rather than producing a
// partition with no shell for curtin to chroot into.
func TestCreateCurtinLandingPartitionImageMissingBusybox(t *testing.T) {
	fs, cleanup, err := vfst.NewTestFS(nil)
	if err != nil {
		t.Fatalf("creating test fs: %v", err)
	}
	defer cleanup()

	cfg := config.NewConfig(config.WithFs(fs), config.WithLogger(internal.Log))
	r := &RawImage{config: cfg}

	if _, err := r.createCurtinLandingPartitionImage(); err == nil {
		t.Fatal("expected error when busybox is not present in the image fs, got nil")
	} else if !strings.Contains(err.Error(), "busybox") {
		t.Fatalf("expected a busybox-related error, got: %v", err)
	}
}
