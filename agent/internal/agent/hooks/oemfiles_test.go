package hook

import (
	"testing"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	implSpec "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

func installSpecWithOEM(mountPoint string) *implSpec.InstallSpec {
	return &implSpec.InstallSpec{
		Partitions: sdkPartitions.ElementalPartitions{
			OEM: &sdkPartitions.Partition{
				FilesystemLabel: constants.OEMLabel,
				MountPoint:      mountPoint,
			},
		},
	}
}

func TestOemFilesDir(t *testing.T) {
	t.Run("returns the mountpoint the installer has OEM on", func(t *testing.T) {
		mounter := v1mock.NewErrorMounter()
		if err := mounter.Mount("/dev/device1", constants.OEMDir, "auto", []string{}); err != nil {
			t.Fatalf("Mount: %v", err)
		}
		c := sdkConfig.Config{Mounter: mounter, Logger: sdkLogger.NewNullLogger()}

		dir, err := oemFilesDir(c, installSpecWithOEM(constants.OEMDir))
		if err != nil {
			t.Fatalf("oemFilesDir() error = %v", err)
		}
		if dir != constants.OEMDir {
			t.Fatalf("oemFilesDir() = %q, want %q", dir, constants.OEMDir)
		}
	})

	t.Run("errors when the OEM partition is not mounted", func(t *testing.T) {
		c := sdkConfig.Config{Mounter: v1mock.NewErrorMounter(), Logger: sdkLogger.NewNullLogger()}

		if _, err := oemFilesDir(c, installSpecWithOEM(constants.OEMDir)); err == nil {
			t.Fatal("oemFilesDir() error = nil, want an error for an unmounted OEM partition")
		}
	})

	t.Run("errors when the spec has no OEM partition", func(t *testing.T) {
		c := sdkConfig.Config{Mounter: v1mock.NewErrorMounter(), Logger: sdkLogger.NewNullLogger()}

		if _, err := oemFilesDir(c, &implSpec.InstallSpec{}); err == nil {
			t.Fatal("oemFilesDir() error = nil, want an error for a spec without an OEM partition")
		}
	})

	t.Run("errors when the OEM partition has no mountpoint", func(t *testing.T) {
		c := sdkConfig.Config{Mounter: v1mock.NewErrorMounter(), Logger: sdkLogger.NewNullLogger()}

		if _, err := oemFilesDir(c, installSpecWithOEM("")); err == nil {
			t.Fatal("oemFilesDir() error = nil, want an error for an OEM partition without a mountpoint")
		}
	})

	t.Run("errors when the spec carries no partition table", func(t *testing.T) {
		c := sdkConfig.Config{Mounter: v1mock.NewErrorMounter(), Logger: sdkLogger.NewNullLogger()}

		var spec sdkSpec.Spec
		if _, err := oemFilesDir(c, spec); err == nil {
			t.Fatal("oemFilesDir() error = nil, want an error for a spec with no partitions")
		}
	})

	t.Run("also resolves the mountpoint from a UKI install spec", func(t *testing.T) {
		// uki/install.go runs the same PostInstall hooks against
		// *implSpec.InstallUkiSpec, not *implSpec.InstallSpec. Both satisfy
		// sdkSpec.SharedInstallSpec, but only InstallSpec was exercised
		// above; this pins the type assertion against the other concrete
		// type production actually passes on a UKI install.
		mounter := v1mock.NewErrorMounter()
		if err := mounter.Mount("/dev/device1", constants.OEMDir, "auto", []string{}); err != nil {
			t.Fatalf("Mount: %v", err)
		}
		c := sdkConfig.Config{Mounter: mounter, Logger: sdkLogger.NewNullLogger()}

		ukiSpec := &implSpec.InstallUkiSpec{
			Partitions: sdkPartitions.ElementalPartitions{
				OEM: &sdkPartitions.Partition{
					FilesystemLabel: constants.OEMLabel,
					MountPoint:      constants.OEMDir,
				},
			},
		}

		dir, err := oemFilesDir(c, ukiSpec)
		if err != nil {
			t.Fatalf("oemFilesDir() error = %v", err)
		}
		if dir != constants.OEMDir {
			t.Fatalf("oemFilesDir() = %q, want %q", dir, constants.OEMDir)
		}
	})
}

func TestOemFileName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "bare name gets the yaml extension",
			input:    "foo",
			expected: "foo.yaml",
		},
		{
			name:     "an explicit .yaml extension is left alone",
			input:    "foo.yaml",
			expected: "foo.yaml",
		},
		{
			name:     "an explicit .yml extension is left alone",
			input:    "foo.yml",
			expected: "foo.yml",
		},
		{
			name:     "a name that merely contains yaml is not treated as having the extension",
			input:    "foo.yaml.bak",
			expected: "foo.yaml.bak.yaml",
		},
		{
			name:    "empty name is rejected",
			input:   "",
			wantErr: true,
		},
		{
			name:    "dot is rejected",
			input:   ".",
			wantErr: true,
		},
		{
			name:    "dot-dot is rejected",
			input:   "..",
			wantErr: true,
		},
		{
			name:    "a path with a separator is rejected",
			input:   "sub/foo",
			wantErr: true,
		},
		{
			name:    "an absolute path is rejected",
			input:   "/etc/passwd",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := oemFileName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("oemFileName(%q) error = nil, want an error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("oemFileName(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Fatalf("oemFileName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
