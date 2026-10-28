package ops

import (
	"testing"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
)

// OEM and recovery are partitions on the raw disk, so they have to carry the
// same filesystem a `kairos-agent install` gives them. Only the image files
// they hold (active.img, passive.img, recovery.img) use the journal-less one.
func TestPartitionImagesUseThePartitionFilesystem(t *testing.T) {
	image := newTestRawImage(nil)

	if got := image.oemPartitionImage(t.TempDir(), t.TempDir(), 0).FS; got != sdkConstants.LinuxFs {
		t.Errorf("the OEM partition is created as %q, want %q", got, sdkConstants.LinuxFs)
	}

	if got := image.recoveryPartitionImage(t.TempDir(), t.TempDir(), 7_974).FS; got != sdkConstants.LinuxFs {
		t.Errorf("the recovery partition is created as %q, want %q", got, sdkConstants.LinuxFs)
	}
}

// The partition still has to hold its recovery image twice over, so moving to
// a journalled filesystem must not have changed how it is sized.
func TestRecoveryPartitionImageKeepsItsSize(t *testing.T) {
	const recoveryImgSize = uint(7_974)

	got := newTestRawImage(nil).recoveryPartitionImage(t.TempDir(), t.TempDir(), recoveryImgSize).Size
	if want := recoveryPartitionSize(recoveryImgSize); got != want {
		t.Errorf("the recovery partition is %d MiB, want %d", got, want)
	}
}
