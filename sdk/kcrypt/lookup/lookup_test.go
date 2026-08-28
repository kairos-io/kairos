package lookup

import (
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

func TestLegacyPartitionName(t *testing.T) {
	tests := []struct {
		label string
		want  string
	}{
		{label: constants.OEMLabel, want: constants.OEMPartName},
		{label: constants.PersistentLabel, want: constants.PersistentPartName},
		{label: "CUSTOM_LABEL", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if got := LegacyPartitionName(tt.label); got != tt.want {
				t.Fatalf("LegacyPartitionName(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

// TestFindByLabelFallsBackToGPTLabel covers installs with no FS label at all
// (kairos-sdk#822 / kairos-io/kairos#4276), where the LUKS container is
// discoverable only via GPT PARTLABEL. The mock intentionally leaves
// FilesystemLabel empty so only the PARTLABEL branch can satisfy the match.
func TestFindByLabelFallsBackToGPTLabel(t *testing.T) {
	var ghwMock mocks.GhwMock
	ghwMock.AddDisk(partitions.Disk{
		Name: "disk",
		Partitions: partitions.PartitionList{
			{
				Name:           "disk1",
				PartitionLabel: constants.OEMPartName,
				FS:             "crypto_LUKS",
			},
		},
	})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	partition, err := FindByLabel(constants.OEMLabel)
	if err != nil {
		t.Fatalf("FindByLabel error = %v", err)
	}
	if partition.Path != "/dev/disk1" {
		t.Fatalf("FindByLabel path = %q, want /dev/disk1", partition.Path)
	}
}

// TestFindByLabelTypeFilters covers the split lookup that lets callers pick
// the LUKS container or the plaintext filesystem unambiguously even when
// both sides share the same label. kairos-io/kairos#4403.
func TestFindByLabelTypeFilters(t *testing.T) {
	var ghwMock mocks.GhwMock
	// LUKS container /dev/vda5 and its unlocked mapper /dev/mapper/vda5
	// both advertise LABEL=COS_PERSISTENT.
	ghwMock.AddDisk(partitions.Disk{
		Name: "vda",
		Partitions: partitions.PartitionList{
			{
				Name:            "vda5",
				PartitionLabel:  constants.PersistentPartName,
				FilesystemLabel: constants.PersistentLabel,
				FS:              "crypto_LUKS",
			},
		},
	})
	ghwMock.AddDisk(partitions.Disk{
		Name: "dm-0",
		Partitions: partitions.PartitionList{
			{
				Name:            "dm-0",
				FilesystemLabel: constants.PersistentLabel,
				FS:              "ext4",
			},
		},
	})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	t.Run("FindLUKSContainerByLabel returns the crypto_LUKS device", func(t *testing.T) {
		p, err := FindLUKSContainerByLabel(constants.PersistentLabel)
		if err != nil {
			t.Fatalf("FindLUKSContainerByLabel error = %v", err)
		}
		if p.FS != "crypto_LUKS" {
			t.Fatalf("FindLUKSContainerByLabel FS = %q, want crypto_LUKS", p.FS)
		}
		if p.Name != "vda5" {
			t.Fatalf("FindLUKSContainerByLabel Name = %q, want vda5", p.Name)
		}
	})

	t.Run("FindMapperByLabel returns the ext4 device, never the LUKS", func(t *testing.T) {
		p, err := FindMapperByLabel(constants.PersistentLabel)
		if err != nil {
			t.Fatalf("FindMapperByLabel error = %v", err)
		}
		if p.FS == "crypto_LUKS" {
			t.Fatalf("FindMapperByLabel returned the LUKS container; must return the plaintext side")
		}
		if p.Name != "dm-0" {
			t.Fatalf("FindMapperByLabel Name = %q, want dm-0", p.Name)
		}
	})
}

// TestMountSourceForLabel pins the resolution that immucore's mount step and
// sdk/machine.Mount depend on to bypass the by-label symlink race.
func TestMountSourceForLabel(t *testing.T) {
	t.Run("prefers the mapper when a LUKS container carries the label", func(t *testing.T) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{
			Name: "vda",
			Partitions: partitions.PartitionList{
				{
					Name:            "vda5",
					PartitionLabel:  constants.PersistentPartName,
					FilesystemLabel: constants.PersistentLabel,
					FS:              "crypto_LUKS",
				},
			},
		})
		ghwMock.CreateDevices()
		t.Cleanup(ghwMock.Clean)

		got, err := MountSourceForLabel(constants.PersistentLabel)
		if err != nil {
			t.Fatalf("MountSourceForLabel error = %v", err)
		}
		if got != "/dev/mapper/vda5" {
			t.Fatalf("MountSourceForLabel = %q, want /dev/mapper/vda5", got)
		}
	})

	t.Run("returns the plain partition when the label lives on unencrypted ext4", func(t *testing.T) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{
			Name: "vda",
			Partitions: partitions.PartitionList{
				{
					Name:            "vda3",
					FilesystemLabel: constants.OEMLabel,
					FS:              "ext4",
				},
			},
		})
		ghwMock.CreateDevices()
		t.Cleanup(ghwMock.Clean)

		got, err := MountSourceForLabel(constants.OEMLabel)
		if err != nil {
			t.Fatalf("MountSourceForLabel error = %v", err)
		}
		if got != "/dev/vda3" {
			t.Fatalf("MountSourceForLabel = %q, want /dev/vda3", got)
		}
	})

	t.Run("returns error when the label is not present", func(t *testing.T) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{Name: "vda"})
		ghwMock.CreateDevices()
		t.Cleanup(ghwMock.Clean)

		if _, err := MountSourceForLabel("NOPE"); err == nil {
			t.Fatalf("MountSourceForLabel expected an error for missing label, got nil")
		}
	})
}
