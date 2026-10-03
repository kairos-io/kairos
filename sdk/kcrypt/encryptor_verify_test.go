package kcrypt

import (
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// The three Unlock implementations verify an unlock with partitionLocked, which
// asks whether the mapper device exists. They used to ask "blkid -L <label>"
// instead. The filesystem label stays on the LUKS container, so that question
// has the same answer before and after the unlock, and the verification could
// never fail. These tests pin the property that made the old check wrong: the
// answer comes from the mapper device, and the labels play no part in it.

func TestPartitionLockedIgnoresTheFilesystemLabel(t *testing.T) {
	// A LUKS container whose mapper device was never created. Both labels are
	// set, and resolve, exactly as they do on a real locked partition.
	locked := &partitions.Partition{
		Name:            "kairos-test-no-such-mapper",
		Path:            "/dev/kairos-test-no-such-mapper",
		PartitionLabel:  "persistent",
		FilesystemLabel: constants.PersistentLUKSLabel,
		FS:              "crypto_LUKS",
	}

	if utils.Exists(lookup.MapperPath(locked)) {
		t.Fatalf("test precondition failed: %s exists, pick another name", lookup.MapperPath(locked))
	}

	if !partitionLocked(locked) {
		t.Errorf("partitionLocked() = false for %s, want true: no mapper device was created, so the unlock did not happen",
			lookup.MapperPath(locked))
	}
}

func TestPartitionLockedOnNilPartition(t *testing.T) {
	if partitionLocked(nil) {
		t.Error("partitionLocked(nil) = true, want false")
	}
}

func TestMapperPathIsKeyedOnTheDeviceName(t *testing.T) {
	// Two partitions that a label lookup cannot tell apart, because a LUKS
	// container and its unlocked mapper carry the same filesystem label.
	first := &partitions.Partition{Name: "vda5", FilesystemLabel: constants.PersistentLUKSLabel}
	second := &partitions.Partition{Name: "vdb5", FilesystemLabel: constants.PersistentLUKSLabel}

	if got, want := lookup.MapperPath(first), "/dev/mapper/vda5"; got != want {
		t.Errorf("MapperPath() = %q, want %q", got, want)
	}
	if lookup.MapperPath(first) == lookup.MapperPath(second) {
		t.Errorf("MapperPath() returned %q for two different devices sharing a label",
			lookup.MapperPath(first))
	}
}

func TestMapperPathOnNilPartition(t *testing.T) {
	if got := lookup.MapperPath(nil); got != "" {
		t.Errorf("MapperPath(nil) = %q, want empty", got)
	}
}
