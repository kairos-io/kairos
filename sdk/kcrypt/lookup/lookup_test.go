package lookup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

func TestOuterLUKSLabel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: constants.PersistentLabel, want: constants.PersistentLUKSLabel},
		{in: constants.OEMLabel, want: constants.OEMLUKSLabel},
		{in: "CUSTOM_LABEL", want: "CUSTOM_LABEL_LUKS"},
		{in: "", want: ""},
		{in: constants.PersistentLUKSLabel, want: constants.PersistentLUKSLabel},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := OuterLUKSLabel(tt.in); got != tt.want {
				t.Fatalf("OuterLUKSLabel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

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

func TestFindByLabelFallsBackToGPTLabel(t *testing.T) {
	var ghwMock mocks.GhwMock
	ghwMock.AddDisk(partitions.Disk{
		Name: "disk",
		Partitions: partitions.PartitionList{
			{
				Name:           "disk1",
				PartitionLabel: "oem",
				FS:             "crypto_LUKS",
			},
		},
	})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	binDir := t.TempDir()
	blkidPath := filepath.Join(binDir, "blkid")
	blkid := "#!/bin/sh\nif [ \"$1\" = \"-L\" ]; then exit 2; fi\nprintf '/dev/disk1\\n'\n"
	if err := os.WriteFile(blkidPath, []byte(blkid), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	partition, err := FindByLabel(constants.OEMLabel)
	if err != nil {
		t.Fatalf("FindByLabel() error = %v", err)
	}
	if partition.Path != "/dev/disk1" {
		t.Fatalf("FindByLabel() path = %q, want /dev/disk1", partition.Path)
	}
}

// TestFindBySuffixedLabel pins the SDK unlock path: findEncryptedPartitions
// surfaces the raw outer FS label (constants.PersistentLUKSLabel), the unlockPartition
// helper hands it right back to FindByLabel, and that call must resolve to
// the same LUKS device. Regressions here would break unlock on every
// post-fix install: the SDK would fail to locate the container it just
// discovered a moment earlier.
//
// The mock leaves PartitionLabel empty so the legacy GPT-name branch cannot
// satisfy the assertion; only the plain-label branch can. Idempotency of
// OuterLUKSLabel is what makes that branch fire for a suffixed input, so a
// future edit that breaks either the idempotency or the plain-label match
// gets caught here.
func TestFindBySuffixedLabel(t *testing.T) {
	var ghwMock mocks.GhwMock
	ghwMock.AddDisk(partitions.Disk{
		Name: "disk",
		Partitions: partitions.PartitionList{
			{
				Name:            "disk1",
				FilesystemLabel: constants.PersistentLUKSLabel,
				FS:              "crypto_LUKS",
			},
		},
	})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	partition, err := FindByLabel(constants.PersistentLUKSLabel)
	if err != nil {
		t.Fatalf("FindByLabel(COS_PERSISTENT_LUKS) error = %v", err)
	}
	if partition.Path != "/dev/disk1" {
		t.Fatalf("FindByLabel() path = %q, want /dev/disk1", partition.Path)
	}
}

// TestFindByLabelFallsBackToOuterLUKS covers immucore's mount step: the
// cos-layout.env asks for LABEL=COS_PERSISTENT, but the LUKS on a post-fix
// install advertises COS_PERSISTENT_LUKS. The outer-label matcher branch
// bridges the two so the mount step can still find the container.
//
// The mock intentionally leaves PartitionLabel empty so the legacy GPT-name
// branch of the matcher cannot satisfy the assertion; only the outer-label
// branch we care about can.
func TestFindByLabelFallsBackToOuterLUKS(t *testing.T) {
	var ghwMock mocks.GhwMock
	ghwMock.AddDisk(partitions.Disk{
		Name: "disk",
		Partitions: partitions.PartitionList{
			{
				Name:            "disk1",
				FilesystemLabel: constants.PersistentLUKSLabel,
				FS:              "crypto_LUKS",
			},
		},
	})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	partition, err := FindByLabel(constants.PersistentLabel)
	if err != nil {
		t.Fatalf("FindByLabel() error = %v", err)
	}
	if partition.Path != "/dev/disk1" {
		t.Fatalf("FindByLabel() path = %q, want /dev/disk1", partition.Path)
	}
}

// TestFindByLabelTypeFilters covers the two type-filtered lookups that let
// callers on pre-fix (colliding-label) installs pick the LUKS container or
// the plaintext filesystem unambiguously. kairos-io/kairos#4403.
func TestFindByLabelTypeFilters(t *testing.T) {
	var ghwMock mocks.GhwMock
	// Simulate a pre-fix install: the LUKS container /dev/vda5 and its
	// unlocked mapper /dev/mapper/vda5 both advertise LABEL=COS_PERSISTENT.
	ghwMock.AddDisk(partitions.Disk{
		Name: "vda",
		Partitions: partitions.PartitionList{
			{
				Name:            "vda5",
				PartitionLabel:  "persistent",
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
					PartitionLabel:  "persistent",
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

	t.Run("prefers the mapper on new installs where outer LUKS has the _LUKS label", func(t *testing.T) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{
			Name: "vda",
			Partitions: partitions.PartitionList{
				{
					Name:            "vda5",
					PartitionLabel:  "persistent",
					FilesystemLabel: constants.PersistentLUKSLabel,
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
