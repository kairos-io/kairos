package kcrypt

import (
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
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
			want: constants.OEMLabel,
		},
		{
			name: "maps legacy persistent GPT label",
			partition: &partitions.Partition{
				FilesystemLabel: "unknown",
				PartitionLabel:  "persistent",
			},
			want: constants.PersistentLabel,
		},
		{
			name: "rejects an unknown partition identity",
			partition: &partitions.Partition{
				FilesystemLabel: "unknown",
				PartitionLabel:  "data",
			},
			want: "",
		},
		{
			// encryptedPartitionLabel returns whatever ghw discovered so the
			// unlocker can locate the same device again by that string. The
			// outer/inner label distinction lives in sdk/kcrypt/lookup.
			name: "returns the raw outer LUKS label unchanged",
			partition: &partitions.Partition{
				FilesystemLabel: constants.PersistentLUKSLabel,
				PartitionLabel:  "persistent",
			},
			want: constants.PersistentLUKSLabel,
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

// TestFindEncryptedPartitionsFallsBackToGPTLabel is the sdk/kcrypt-level
// smoke: given a LUKS partition with no FS label but a GPT PartitionLabel
// (a pre-kairos-sdk#822 shape, kairos-io/kairos#4276), findEncryptedPartitions
// should still surface it via encryptedPartitionLabel's GPT mapping.
func TestFindEncryptedPartitionsFallsBackToGPTLabel(t *testing.T) {
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

	labels, err := findEncryptedPartitions(logger.NewNullLogger())
	if err != nil {
		t.Fatalf("findEncryptedPartitions() error = %v", err)
	}
	if len(labels) != 1 || labels[0] != constants.OEMLabel {
		t.Fatalf("findEncryptedPartitions() = %v, want [COS_OEM]", labels)
	}
}

// TestFindEncryptedPartitionsReturnsRawOuterLabel: on a post-#4403 install
// the LUKS container advertises COS_PERSISTENT_LUKS; findEncryptedPartitions
// surfaces that raw label so the unlocker can locate the same partition
// again via lookup.FindByLabel.
func TestFindEncryptedPartitionsReturnsRawOuterLabel(t *testing.T) {
	var ghwMock mocks.GhwMock
	ghwMock.AddDisk(partitions.Disk{
		Name: "disk",
		Partitions: partitions.PartitionList{
			{
				Name:            "disk1",
				PartitionLabel:  "persistent",
				FilesystemLabel: constants.PersistentLUKSLabel,
				FS:              "crypto_LUKS",
			},
		},
	})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	labels, err := findEncryptedPartitions(logger.NewNullLogger())
	if err != nil {
		t.Fatalf("findEncryptedPartitions() error = %v", err)
	}
	if len(labels) != 1 || labels[0] != constants.PersistentLUKSLabel {
		t.Fatalf("findEncryptedPartitions() = %v, want [COS_PERSISTENT_LUKS]", labels)
	}
}
