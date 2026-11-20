package store

import (
	"context"
	"errors"
	"time"
)

// Batch phases.
const (
	BatchRunning   = "running"
	BatchCompleted = "completed"
	BatchFailed    = "failed"
	BatchStopped   = "stopped"
)

// Batch node states. A node moves waiting, running, then rebooting when its
// command reboots the machine, and ends in one of the final states.
const (
	BatchNodeWaiting   = "waiting"
	BatchNodeRunning   = "running"
	BatchNodeRebooting = "rebooting"
	BatchNodeDone      = "done"
	BatchNodeFailed    = "failed"
	BatchNodeTimedOut  = "timed_out"
	BatchNodeSkipped   = "skipped"
	BatchNodeCanceled  = "canceled"
)

// DefaultRebootTimeoutSeconds is how long a rebooting node may take to come
// back when the batch does not say.
const DefaultRebootTimeoutSeconds = 600

// MaxRebootTimeoutSeconds is the longest a batch may wait for a rebooting node
// to come back: 7 days.
const MaxRebootTimeoutSeconds = 7 * 24 * 60 * 60

// IsFinalBatchNodeState reports whether a node in state takes no further part
// in its batch.
func IsFinalBatchNodeState(state string) bool {
	switch state {
	case BatchNodeDone, BatchNodeFailed, BatchNodeTimedOut, BatchNodeSkipped, BatchNodeCanceled:
		return true
	}
	return false
}

// RebootsNode reports whether AuroraBoot reboots the node for command: upgrade, reset, reboot.
func RebootsNode(command string) bool {
	switch command {
	case CmdUpgrade, CmdReset, CmdReboot:
		return true
	}
	return false
}

// CommandBatch is a rolling run of one command over a set of nodes.
type CommandBatch struct {
	ID                   string            `json:"id" gorm:"primaryKey"`
	Command              string            `json:"command"`
	Args                 map[string]string `json:"args" gorm:"serializer:json"`
	Selection            string            `json:"selection"`
	GroupID              *string           `json:"groupID" gorm:"index" extensions:"x-nullable"`
	Concurrency          int               `json:"concurrency"`
	StopOnFailure        bool              `json:"stopOnFailure"`
	RebootTimeoutSeconds int               `json:"rebootTimeoutSeconds"`
	Phase                string            `json:"phase" gorm:"index"`
	CreatedAt            time.Time         `json:"createdAt"`
	FinishedAt           *time.Time        `json:"finishedAt"`
	StoppedAt            *time.Time        `json:"stoppedAt"`
	StopReason           string            `json:"stopReason"`
	StoppedByNodeID      *string           `json:"stoppedByNodeID" extensions:"x-nullable"`
	// Counts maps each node state to how many nodes are in it.
	Counts map[string]int `json:"counts" gorm:"-"`
	// Hostnames holds the hostnames of the batch's first five nodes in start
	// order (BatchListHostnames). Only the batch list fills it.
	Hostnames []string `json:"hostnames,omitempty" gorm:"-"`
	// Nodes is filled by BatchGet only.
	Nodes []*BatchNode `json:"nodes,omitempty" gorm:"foreignKey:BatchID"`
}

// BatchNode is one node's place in a batch.
type BatchNode struct {
	BatchID   string     `json:"batchID" gorm:"primaryKey"`
	NodeID    string     `json:"nodeID" gorm:"primaryKey;index"`
	Position  int        `json:"position"`
	Hostname  string     `json:"hostname"`
	CommandID *string    `json:"commandID" gorm:"index" extensions:"x-nullable"`
	State     string     `json:"state" gorm:"index"`
	Message   string     `json:"message"`
	StartedAt *time.Time `json:"startedAt"`
	// RebootingSince is when the reboot the node is waiting on was
	// scheduled: its command's completion, then its autoreset boot.
	RebootingSince *time.Time `json:"rebootingSince"`
	Deadline       *time.Time `json:"deadline"`
	FinishedAt     *time.Time `json:"finishedAt"`
}

// TableName names the per-node table of a batch.
func (BatchNode) TableName() string { return "command_batch_nodes" }

// BatchListHostnames is how many hostnames BatchList returns per batch.
const BatchListHostnames = 5

// BatchFilter narrows BatchStore.List.
type BatchFilter struct {
	GroupID string
	NodeID  string
	Limit   int // 0 means 50
	Offset  int
}

var (
	ErrBatchNotFound   = errors.New("batch not found")
	ErrBatchNotRunning = errors.New("batch is not running")
)

// BatchStore persists batches and their per-node rows.
type BatchStore interface {
	// Create writes the batch and its node rows in one transaction. Every row
	// must be waiting; Phase is set to running.
	Create(ctx context.Context, b *CommandBatch, nodes []*BatchNode) error
	// Get returns the batch with Nodes ordered by Position and Counts filled.
	Get(ctx context.Context, id string) (*CommandBatch, error)
	// List returns batches newest first with Counts filled and Nodes empty,
	// and the total number matching the filter.
	List(ctx context.Context, f BatchFilter) ([]*CommandBatch, int, error)
	// BusyNodes returns the IDs among nodeIDs that have a row in a non-final state.
	BusyNodes(ctx context.Context, nodeIDs []string) ([]string, error)
	// Advance brings one batch up to date at now and returns the commands it
	// created, for the caller to push after the transaction committed.
	Advance(ctx context.Context, batchID string, now time.Time) ([]*NodeCommand, error)
	// Stop stops a running batch: phase stopped, StoppedAt now, StopReason
	// reason, StoppedByNodeID nil, and every waiting row canceled with
	// "batch stopped". ErrBatchNotRunning when the phase is not running.
	Stop(ctx context.Context, batchID, reason string, now time.Time) error
	// NodeBooted settles the node's rebooting rows when it came back: bootedAt
	// is the boot time a heartbeat reports, or nil for a registration, which
	// always follows a boot. It returns the IDs of the batches it touched.
	NodeBooted(ctx context.Context, nodeID, bootState string, bootedAt *time.Time, now time.Time) ([]string, error)
	// ActiveIDs returns the batches that are running or still have a running or
	// rebooting row.
	ActiveIDs(ctx context.Context) ([]string, error)
	// PostponeExpiredDeadlines moves every rebooting deadline before now to now
	// plus its batch's timeout.
	PostponeExpiredDeadlines(ctx context.Context, now time.Time) error
}
