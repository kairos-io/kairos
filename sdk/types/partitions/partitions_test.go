package partitions

import (
	"testing"

	"github.com/kairos-io/kairos-sdk/constants"
)

func TestSetFirmwarePartitionsEFIDefaultSize(t *testing.T) {
	ep := &ElementalPartitions{}
	if err := ep.SetFirmwarePartitions(constants.EFI, constants.GPT); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.EFI == nil {
		t.Fatal("expected an EFI partition to be set")
	}
	if ep.EFI.Size != constants.EfiSize {
		t.Errorf("expected default EFI size %d, got %d", constants.EfiSize, ep.EFI.Size)
	}
}

func TestSetFirmwarePartitionsEFIPreservesConfiguredSize(t *testing.T) {
	const configured = uint(128)
	ep := &ElementalPartitions{EFI: &Partition{Size: configured}}
	if err := ep.SetFirmwarePartitions(constants.EFI, constants.GPT); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.EFI.Size != configured {
		t.Errorf("expected configured EFI size %d to be preserved, got %d", configured, ep.EFI.Size)
	}
	// The rest of the fields must still be forced to their fixed values, since
	// boot relies on the label/name/mountpoint being predictable.
	if ep.EFI.FilesystemLabel != constants.EfiLabel {
		t.Errorf("expected label %q, got %q", constants.EfiLabel, ep.EFI.FilesystemLabel)
	}
	if ep.EFI.MountPoint != constants.EfiDirTransient {
		t.Errorf("expected mountpoint %q, got %q", constants.EfiDirTransient, ep.EFI.MountPoint)
	}
}

func TestSetFirmwarePartitionsEFIZeroFallsBackToDefault(t *testing.T) {
	ep := &ElementalPartitions{EFI: &Partition{Size: 0}}
	if err := ep.SetFirmwarePartitions(constants.EFI, constants.GPT); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep.EFI.Size != constants.EfiSize {
		t.Errorf("expected fallback to default %d for zero size, got %d", constants.EfiSize, ep.EFI.Size)
	}
}
