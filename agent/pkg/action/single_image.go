package action

import (
	"fmt"

	"github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils/partitions"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// NoRecoveryResetMessage is what reset says on a node without a recovery
// partition. Reset runs from the recovery system, so there is nothing to run
// it from.
const NoRecoveryResetMessage = "This node has no recovery partition (it was built with disk.no_recovery), so reset is not supported. Replace the node with a fresh disk."

// IsSingleImageNode reports whether the node is one built with a single
// system image and no recovery partition (AuroraBoot's disk.no_recovery).
//
// ASSUMPTION: a node without a COS_RECOVERY partition is a single image node
// built with disk.no_recovery. That holds because the AuroraBoot schema only
// allows disk.no_recovery together with disk.state_slots=1 (see
// validateBootActive in auroraboot/pkg/schema/config.go), and no other
// supported way of building a disk leaves COS_RECOVERY out. A disk that lost
// its recovery partition some other way gets the same wording, which is fine
// because the messages state a fact about the disk. If the schema ever allows
// no_recovery with more slots, or a node can be recovery-less for another
// reason, this inference and the messages built on it have to change.
func IsSingleImageNode(recovery *sdkPartitions.Partition) bool {
	return recovery == nil
}

// CheckResetSupported fails when the node has no recovery partition.
func CheckResetSupported(recovery *sdkPartitions.Partition) error {
	if IsSingleImageNode(recovery) {
		return fmt.Errorf("%s", NoRecoveryResetMessage)
	}
	return nil
}

// FindRecoveryPartition looks up COS_RECOVERY the way the upgrade and reset
// specs do: in the partition list first, then behind device mapper, where an
// LVM layout hides it from the list. It returns nil when there is none.
func FindRecoveryPartition(cfg *sdkConfig.Config) *sdkPartitions.Partition {
	parts, err := partitions.GetAllPartitions(&cfg.Logger)
	if err != nil {
		cfg.Logger.Debugf("could not read host partitions: %s", err)
	}
	ep := spec.NewElementalPartitionsFromList(parts)
	if ep.Recovery == nil {
		ep.Recovery = partitions.GetPartitionViaDM(cfg.Fs, sdkConstants.RecoveryLabel)
	}
	return ep.Recovery
}
