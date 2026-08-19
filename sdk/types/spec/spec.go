package spec

import "github.com/kairos-io/kairos-sdk/types/partitions"

// Spec is the interface that all specs need to implement.
type Spec interface {
	Sanitize() error
	ShouldReboot() bool
	ShouldShutdown() bool
}

// SharedInstallSpec is the interface that Install specs need to implement.
type SharedInstallSpec interface {
	GetPartTable() string
	GetTarget() string
	GetPartitions() partitions.ElementalPartitions
	GetExtraPartitions() partitions.PartitionList
}
