package ops

import (
	"testing"

	"github.com/kairos-io/kairos/v4/agent/pkg/partitioner"
	"github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
)

// GRUB reads COS_OEM and COS_RECOVERY before a kernel exists, and a GRUB older
// than 2.12 without the RHEL backport refuses an ext filesystem carrying
// metadata_csum_seed. The raw disk builder never calls mkfs itself: it reaches
// it through elemental.CreateFileSystemImage, so whether its partitions are
// readable is decided by the kairos/v4 version in go.mod, not by anything in
// this repo. A downgrade past kairos-io/kairos#5472 would silently ship
// unbootable raw disks again, so assert the argv from here.
func TestPartitionMkfsTurnsOffMetadataCsumSeed(t *testing.T) {
	for _, fs := range []string{sdkConstants.LinuxFs, sdkConstants.LinuxImgFs} {
		runner := mocks.NewFakeRunner()

		if _, err := partitioner.NewMkfsCall("/dev/device1", fs, "COS_OEM", runner).Apply(); err != nil {
			t.Fatalf("mkfs.%s: %v", fs, err)
		}

		if err := runner.IncludesCmds([][]string{
			{"mkfs." + fs, "-L", "COS_OEM", "-O", sdkConstants.ExtDisabledFeatures},
		}); err != nil {
			t.Errorf("mkfs.%s does not disable %s: %v", fs, sdkConstants.ExtDisabledFeatures, err)
		}
	}
}

// The ESP is the one filesystem AuroraBoot formats itself, and mkfs.msdos has
// no -O. Guards the regex in buildOptions from growing to cover it.
func TestEspMkfsIsLeftAlone(t *testing.T) {
	runner := mocks.NewFakeRunner()

	if _, err := partitioner.NewMkfsCall("/dev/device1", "vfat", "COS_GRUB", runner).Apply(); err != nil {
		t.Fatalf("mkfs.vfat: %v", err)
	}

	if err := runner.IncludesCmds([][]string{{"mkfs.vfat", "-n", "COS_GRUB", "-O"}}); err == nil {
		t.Error("mkfs.vfat was given -O, which it does not accept")
	}

	if err := runner.CmdsMatch([][]string{{"mkfs.vfat", "-n", "COS_GRUB", "/dev/device1"}}); err != nil {
		t.Errorf("mkfs.vfat argv: %v", err)
	}
}
