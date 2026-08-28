package state

import (
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// TestLabelFromByLabelPath pins the small string helper immucore's mount step
// uses to unwrap a /dev/disk/by-label/<X> path back to its label component.
// The mount step calls resolveMountSource with that label so the concrete
// device path (mapper for LUKS, plain partition otherwise) replaces the
// racy by-label form in both the mount(2) call and the fstab entry.
func TestLabelFromByLabelPath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "/dev/disk/by-label/COS_PERSISTENT", want: constants.PersistentLabel},
		{in: "/dev/disk/by-label/COS_OEM", want: constants.OEMLabel},
		{in: "/dev/disk/by-label/MYAPP_DATA", want: "MYAPP_DATA"},
		{in: "/dev/sda2", want: ""},
		{in: "/dev/mapper/vda5", want: ""},
		{in: "LABEL=COS_PERSISTENT", want: ""},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := labelFromByLabelPath(tt.in); got != tt.want {
				t.Fatalf("labelFromByLabelPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestResolveMountSource verifies the resolution path immucore's mount step
// takes for encrypted mounts on a pre-fix install (LUKS container and its
// inner ext4 both labelled COS_PERSISTENT). The mount source must be the
// mapper (see kairos-io/kairos#4403), so udev's by-label race doesn't decide
// what gets mounted.
func TestResolveMountSource(t *testing.T) {
	t.Run("encrypted: resolves to the mapper path", func(t *testing.T) {
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

		got, err := resolveMountSource(constants.PersistentLabel)
		if err != nil {
			t.Fatalf("resolveMountSource(COS_PERSISTENT) error = %v", err)
		}
		if got != "/dev/mapper/vda5" {
			t.Fatalf("resolveMountSource(COS_PERSISTENT) = %q, want /dev/mapper/vda5", got)
		}
	})

	t.Run("plain: resolves to the partition path", func(t *testing.T) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{
			Name: "vda",
			Partitions: partitions.PartitionList{
				{
					Name:            "vda2",
					FilesystemLabel: constants.OEMLabel,
					FS:              "ext4",
				},
			},
		})
		ghwMock.CreateDevices()
		t.Cleanup(ghwMock.Clean)

		got, err := resolveMountSource(constants.OEMLabel)
		if err != nil {
			t.Fatalf("resolveMountSource(COS_OEM) error = %v", err)
		}
		if got != "/dev/vda2" {
			t.Fatalf("resolveMountSource(COS_OEM) = %q, want /dev/vda2", got)
		}
	})

	t.Run("unknown label: returns an error", func(t *testing.T) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{Name: "vda"})
		ghwMock.CreateDevices()
		t.Cleanup(ghwMock.Clean)

		got, err := resolveMountSource("MISSING_LABEL")
		if err == nil {
			t.Fatalf("resolveMountSource(MISSING_LABEL) = %q, want an error", got)
		}
	})
}
