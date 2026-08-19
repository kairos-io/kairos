package kcrypt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos-sdk/ghw/mocks"
	"github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
)

func TestEncryptedPartitionLabel(t *testing.T) {
	tests := []struct {
		name      string
		partition *partitions.Partition
		want      string
	}{
		{
			name: "keeps a LUKS filesystem label",
			partition: &partitions.Partition{
				FilesystemLabel: "CUSTOM_LABEL",
				PartitionLabel:  "oem",
			},
			want: "CUSTOM_LABEL",
		},
		{
			name: "maps legacy OEM GPT label",
			partition: &partitions.Partition{
				FilesystemLabel: "unknown",
				PartitionLabel:  "oem",
			},
			want: "COS_OEM",
		},
		{
			name: "maps legacy persistent GPT label",
			partition: &partitions.Partition{
				FilesystemLabel: "unknown",
				PartitionLabel:  "persistent",
			},
			want: "COS_PERSISTENT",
		},
		{
			name: "rejects an unknown partition identity",
			partition: &partitions.Partition{
				FilesystemLabel: "unknown",
				PartitionLabel:  "data",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := encryptedPartitionLabel(tt.partition); got != tt.want {
				t.Fatalf("encryptedPartitionLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLegacyPartitionName(t *testing.T) {
	tests := []struct {
		label string
		want  string
	}{
		{label: "COS_OEM", want: "oem"},
		{label: "COS_PERSISTENT", want: "persistent"},
		{label: "CUSTOM_LABEL", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if got := legacyPartitionName(tt.label); got != tt.want {
				t.Fatalf("legacyPartitionName(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

func TestFindPartitionByLabelFallsBackToGPTLabel(t *testing.T) {
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

	partition, err := findPartitionByLabel("COS_OEM")
	if err != nil {
		t.Fatalf("findPartitionByLabel() error = %v", err)
	}
	if partition.Path != "/dev/disk1" {
		t.Fatalf("findPartitionByLabel() path = %q, want /dev/disk1", partition.Path)
	}

	labels, err := findEncryptedPartitions(logger.NewNullLogger())
	if err != nil {
		t.Fatalf("findEncryptedPartitions() error = %v", err)
	}
	if len(labels) != 1 || labels[0] != "COS_OEM" {
		t.Fatalf("findEncryptedPartitions() = %v, want [COS_OEM]", labels)
	}
}
