package store

import (
	"context"
	"errors"
	"time"
)

// ErrCommandNotFound is returned by command status updates when no command
// matches the id, or, for node-scoped updates, both the command id and the
// owning node. Callers use it to fail closed (404/403) rather than silently
// succeeding on a foreign or non-existent command.
var ErrCommandNotFound = errors.New("command not found")

// ErrCommandTerminal is returned by command status updates when the command
// already reached a terminal phase other than the one reported. A terminal
// command is never moved again; repeating the phase it ended in is a no-op.
var ErrCommandTerminal = errors.New("command already reached a terminal phase")

// ErrNoClaimCapacity is returned by NodeStore.ClaimNode when the target group
// has no unclaimed node available. It is a distinct, expected outcome — not a
// failure — so a caller (e.g. a CAPI infrastructure provider) can surface
// "waiting for capacity" and requeue instead of treating it as an error.
var ErrNoClaimCapacity = errors.New("no unclaimed node available in group")

// ErrGroupNameTaken is returned by GroupStore.Create and GroupStore.Update when
// another group already uses the name.
var ErrGroupNameTaken = errors.New("group name already in use")

// NodeGroup represents a logical group/environment for nodes (e.g., "production", "staging").
type NodeGroup struct {
	ID          string `json:"id" gorm:"primaryKey"`
	Name        string `json:"name" gorm:"uniqueIndex"`
	Description string `json:"description"`
	// NodeCount is the number of nodes in the group. It is not stored. List and
	// Get always set it (0 included); a group embedded in a node payload leaves
	// it nil, so it is omitted there.
	NodeCount *int      `json:"node_count,omitempty" gorm:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ManagedNode represents a Kairos node managed by auroraboot.
// NodeAddress is one network address a node reports, mirroring CAPI's
// MachineAddress (a {type, address} pair) so a CAPI infrastructure provider can
// pass it straight through to Machine.status.addresses. Nodes report a LIST:
// multi-NIC is normal. Type is one of InternalIP | ExternalIP | Hostname (kept as
// a plain string so a future type passes through without a server change).
//
// The agent decides which interfaces to report (loopback/bridge/veth/bond
// filtering is the agent's job); the server stores the list exactly as received.
// The shape also leaves room for a future externally-supplied ExternalIP that is
// not on any NIC — no injection path is built for that here.
type NodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type ManagedNode struct {
	ID            string            `json:"id" gorm:"primaryKey"`
	MachineID     string            `json:"machineID" gorm:"uniqueIndex"`
	Hostname      string            `json:"hostname"`
	GroupID       *string           `json:"groupID" gorm:"index;index:idx_node_group_claim,unique,priority:1" extensions:"x-nullable"`
	Group         *NodeGroup        `json:"group,omitempty" gorm:"foreignKey:GroupID;constraint:OnDelete:SET NULL"`
	Phase         string            `json:"phase"`
	LastHeartbeat *time.Time        `json:"lastHeartbeat"`
	AgentVersion  string            `json:"agentVersion"`
	OSRelease     map[string]string `json:"osRelease" gorm:"serializer:json"`
	Labels        map[string]string `json:"labels" gorm:"serializer:json"`
	// Addresses are the node's reported network addresses (multi-NIC). Optional
	// and backward-compatible: an agent that does not send them leaves the list
	// empty. Stored as JSON, mirroring OSRelease/Labels.
	Addresses []NodeAddress `json:"addresses,omitempty" gorm:"serializer:json"`
	// RemoteIP is the IP the server observed the node connect from at register or
	// heartbeat time (kairos-io/kairos#4590), independent of Addresses: the node
	// does not report it, and it survives even when an agent sends no addresses at
	// all (e.g. the WebSocket heartbeat path, or an older agent). It tracks NAT/DHCP
	// changes because every heartbeat re-observes and overwrites it.
	RemoteIP string `json:"remoteIP,omitempty"`
	// BootState is the node's reported boot state for day-2 lifecycle (e.g. a node
	// that booted the passive image signals a broken active image). Known values:
	// active | passive | recovery | autoreset — but unknown values are accepted
	// and stored as-is so future boot states pass through without a server change.
	BootState string `json:"bootState,omitempty"`
	// ClaimKey, when non-nil, is the opaque caller-owned key that has claimed this
	// node via POST /api/v1/groups/:id/claim (e.g. a CAPI machine's identity). A
	// nil ClaimKey means the node is unclaimed and available. It is a pointer, not
	// a string, on purpose: "unclaimed" must be SQL NULL, because the composite
	// unique index idx_node_group_claim on (group_id, claim_key) makes a claimKey
	// own at most one node per group, and NULLs are exempt from unique indexes on
	// both SQLite and PostgreSQL — so many unclaimed nodes coexist while a second
	// node can never take an already-used claimKey in the same group.
	ClaimKey *string `json:"claimKey,omitempty" gorm:"index:idx_node_group_claim,unique,priority:2"`
	// ClaimedAt is when ClaimKey was set; nil when unclaimed.
	ClaimedAt *time.Time `json:"claimedAt,omitempty"`
	// ResetState tracks the day-2 automatic-reset lifecycle across the reboot a
	// reset command triggers (kairos-io/kairos#4255). A reset can't complete
	// synchronously — the agent selects the statereset boot entry and reboots — so
	// the command acks immediately and the real outcome is resolved here when the
	// node re-registers and reports its post-reboot boot state. One of:
	// "" (none) | pending | in-progress | done | failed.
	ResetState string `json:"resetState,omitempty"`
	// ResetRequestedAt is when the reset command was issued (ResetState set to
	// pending); nil when no reset has been requested.
	ResetRequestedAt *time.Time `json:"resetRequestedAt,omitempty"`
	// ResetProgressAt is when the reset last made observable progress. Two
	// events stamp it: the node claiming the reset command for delivery, and
	// the node coming back reporting bootState=autoreset, by re-registering or
	// by a heartbeat after the reset reboot (the lifecycle moving from pending
	// to in-progress). The reset timeout is measured from
	// this timestamp when it is set, so a node that picks the command up late
	// still gets a full window to reboot, wipe and come back instead of sharing
	// the one that started at ResetRequestedAt. Nil until the reset reaches the
	// node, and cleared when a new reset is requested.
	ResetProgressAt *time.Time `json:"resetProgressAt,omitempty"`
	// ResetRebootAt is when the agent reported the pending reset command
	// Completed, which is when it scheduled the reboot. A heartbeat from a boot
	// after it settles the reset. It is kept on the node, not read from the
	// command, so clearing the command history cannot strand the reset.
	ResetRebootAt *time.Time `json:"-"`
	// LastReset is when the most recent automatic reset completed successfully.
	LastReset *time.Time `json:"lastReset,omitempty"`
	// UpgradeState tracks the upgrade lifecycle across the reboot an upgrade
	// command triggers. The agent acks the command once the new image is staged,
	// before it reboots into it, so a Completed upgrade command only means the
	// upgrade was scheduled; the real outcome is resolved here when the node
	// re-registers and reports its post-reboot boot state. One of:
	// "" (none) | pending | done | failed.
	UpgradeState string `json:"upgradeState,omitempty"`
	// UpgradeRequestedAt is when the upgrade command was issued (UpgradeState set
	// to pending); nil when no upgrade has been requested.
	UpgradeRequestedAt *time.Time `json:"upgradeRequestedAt,omitempty"`
	// UpgradeRebootAt is when the agent reported the pending upgrade command
	// Completed, which is when it scheduled the reboot. A heartbeat from a boot
	// after it settles the upgrade, for the same reason as ResetRebootAt.
	UpgradeRebootAt *time.Time `json:"-"`
	// LastUpgrade is when the most recent upgrade completed successfully.
	LastUpgrade *time.Time `json:"lastUpgrade,omitempty"`
	APIKey      string     `json:"-" gorm:"index"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// Reported boot states (ManagedNode.BootState) — the short fleet vocabulary the
// agent sends on register/heartbeat (kairos-io/kairos#4253). Unknown values are
// still stored as-is; these are the ones the reset lifecycle reasons about.
const (
	BootStateActive    = "active"
	BootStatePassive   = "passive"
	BootStateRecovery  = "recovery"
	BootStateAutoReset = "autoreset"
)

// Reset states (ManagedNode.ResetState) for the day-2 automatic-reset lifecycle
// (kairos-io/kairos#4255). CommandStore.CreateBatch sets `pending` when a reset
// command is issued; the re-register resolver advances it from the reported boot state:
// autoreset → in-progress, active → done, passive/recovery → failed. A reset
// command that reports Failed while the reset is still pending also moves it to
// failed.
const (
	ResetStatePending    = "pending"
	ResetStateInProgress = "in-progress"
	ResetStateDone       = "done"
	ResetStateFailed     = "failed"
)

// Upgrade states (ManagedNode.UpgradeState) for the upgrade lifecycle.
// CommandStore.CreateBatch sets `pending` when an upgrade command is issued; the
// re-register resolver advances it from the reported boot state: active → done,
// passive/recovery → failed (the node fell back off the upgraded image). An
// upgrade command that reports Failed before the reboot also moves it to failed.
const (
	UpgradeStatePending = "pending"
	UpgradeStateDone    = "done"
	UpgradeStateFailed  = "failed"
)

// Node phases.
const (
	PhasePending    = "Pending"
	PhaseRegistered = "Registered"
	PhaseOnline     = "Online"
	PhaseOffline    = "Offline"
)

// NodeCommand represents a command queued for a node.
type NodeCommand struct {
	ID            string            `json:"id" gorm:"primaryKey"`
	ManagedNodeID string            `json:"managedNodeID" gorm:"index"`
	Command       string            `json:"command"`
	Args          map[string]string `json:"args" gorm:"serializer:json"`
	Phase         string            `json:"phase"`
	Result        string            `json:"result"`
	ExpiresAt     *time.Time        `json:"expiresAt"`
	DeliveredAt   *time.Time        `json:"deliveredAt"`
	CompletedAt   *time.Time        `json:"completedAt"`
	CreatedAt     time.Time         `json:"createdAt"`

	// BatchID names the batch that created this command, so a command can be
	// traced to its batch. It is empty on a command addressed to one node.
	BatchID string `json:"batchID,omitempty" gorm:"index"`
}

// Command phases.
const (
	CommandPending   = "Pending"
	CommandDelivered = "Delivered"
	CommandRunning   = "Running"
	CommandCompleted = "Completed"
	CommandFailed    = "Failed"
	CommandExpired   = "Expired"
	// CommandCanceled is a command that will never be delivered. It is
	// terminal and distinct from Failed: the node never ran anything.
	CommandCanceled = "Canceled"
)

// TerminalCommandPhases are the phases a command can no longer move out of.
var TerminalCommandPhases = []string{CommandCompleted, CommandFailed, CommandExpired, CommandCanceled}

// IsValidCommandPhase reports whether phase is one of the known command phases.
func IsValidCommandPhase(phase string) bool {
	switch phase {
	case CommandPending, CommandDelivered, CommandRunning,
		CommandCompleted, CommandFailed, CommandExpired, CommandCanceled:
		return true
	}
	return false
}

// IsTerminalCommandPhase reports whether a phase is one the command can no
// longer move out of, so a caller can tell the rows that still have work
// ahead of them from the ones that are only history.
func IsTerminalCommandPhase(phase string) bool {
	switch phase {
	case CommandCompleted, CommandFailed, CommandExpired, CommandCanceled:
		return true
	}
	return false
}

// Command types.
const (
	CmdUpgrade          = "upgrade"
	CmdReset            = "reset"
	CmdExec             = "exec"
	CmdApplyCloudConfig = "apply-cloud-config"
	CmdUpgradeRecovery  = "upgrade-recovery"
	CmdReboot           = "reboot"
	CmdExtension        = "extension"
)

// CommandSelector targets nodes for bulk command operations.
type CommandSelector struct {
	GroupID string            `json:"groupID,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	NodeIDs []string          `json:"nodeIDs,omitempty"`
}

// GroupStore manages node groups.
type GroupStore interface {
	Create(ctx context.Context, group *NodeGroup) error
	GetByID(ctx context.Context, id string) (*NodeGroup, error)
	GetByName(ctx context.Context, name string) (*NodeGroup, error)
	List(ctx context.Context) ([]*NodeGroup, error)
	Update(ctx context.Context, group *NodeGroup) error
	Delete(ctx context.Context, id string) error
	// NodeCounts returns the number of nodes per group ID. Groups with no
	// nodes have no key.
	NodeCounts(ctx context.Context) (map[string]int, error)
}

// NodeStore manages node registration and state.
type NodeStore interface {
	Register(ctx context.Context, node *ManagedNode) error
	GetByID(ctx context.Context, id string) (*ManagedNode, error)
	GetByMachineID(ctx context.Context, machineID string) (*ManagedNode, error)
	GetByAPIKey(ctx context.Context, apiKey string) (*ManagedNode, error)
	List(ctx context.Context) ([]*ManagedNode, error)
	ListByGroup(ctx context.Context, groupID string) ([]*ManagedNode, error)
	ListByLabels(ctx context.Context, labels map[string]string) ([]*ManagedNode, error)
	ListBySelector(ctx context.Context, sel CommandSelector) ([]*ManagedNode, error)
	// UpdateHeartbeat records a heartbeat: it stamps LastHeartbeat, moves the node
	// to Online, and applies whatever the report carried. agentVersion and
	// osRelease are always written; addresses, bootState, hostname and remoteIP are
	// only written when non-empty, so a caller that does not collect a field (an
	// older agent, or a transport that does not carry it) preserves the stored
	// value instead of blanking it.
	UpdateHeartbeat(ctx context.Context, id string, agentVersion string, osRelease map[string]string, addresses []NodeAddress, bootState string, hostname string, remoteIP string) error
	UpdatePhase(ctx context.Context, id string, phase string) error
	// SetGroup moves nodeID into groupID, or out of any group when groupID
	// is nil.
	SetGroup(ctx context.Context, nodeID string, groupID *string) error
	SetLabels(ctx context.Context, nodeID string, labels map[string]string) error
	// ClaimNode atomically assigns one unclaimed node in groupID to claimKey and
	// returns it. It is idempotent: re-issuing with the same (groupID, claimKey)
	// returns the SAME node, never a second one — which is what makes a
	// level-triggered CAPI reconcile safe across controller restarts and retries.
	// Concurrent claims are arbitrated by the store so two callers never receive
	// the same node. Returns ErrNoClaimCapacity when the group has no unclaimed
	// node available.
	ClaimNode(ctx context.Context, groupID, claimKey string) (*ManagedNode, error)
	// ReleaseNode clears the claim on nodeID, returning it to the group's pool for
	// reuse, but only if the claim is currently held by claimKey. released reports
	// whether a claim was actually cleared: (false, nil) means the node was not
	// claimed by this key (it is already free, or owned by a different key). The
	// caller inspects the node to distinguish those and must not assume ownership
	// from a false result.
	ReleaseNode(ctx context.Context, nodeID, claimKey string) (released bool, err error)
	// AdvanceReset atomically transitions nodeID's ResetState from any of
	// fromStates to `to`, returning true only if this call performed the
	// transition — so concurrent re-registers resolve exactly once. When
	// stampLastReset is set (the transition to done), LastReset is set to now in
	// the same update. The transition to in-progress additionally stamps
	// ResetProgressAt, because reaching it means the node re-registered. A
	// transition to failed clears ResetRebootAt, so a later boot does not
	// settle the failure as done.
	AdvanceReset(ctx context.Context, nodeID string, fromStates []string, to string, stampLastReset bool) (bool, error)
	// StampResetProgress refreshes ResetProgressAt to now, but only while the
	// reset is still in flight (pending or in-progress), so a late delivery can
	// never re-anchor a reset that already resolved. It returns true only if
	// this call stamped. Called when the node claims the reset command: taking
	// delivery is the first proof the reset reached the node, and the node
	// needs a full window from that moment to reboot, wipe and come back.
	StampResetProgress(ctx context.Context, nodeID string) (bool, error)
	// FailResetBefore atomically marks an in-flight reset failed only when both
	// its request timestamp and its progress timestamp are at or before the
	// supplied deadline. Including them in the compare-and-set prevents an older
	// read from failing a reset that has since been refreshed concurrently,
	// either by a new request (ResetRequestedAt) or by the reset making progress
	// (ResetProgressAt).
	FailResetBefore(ctx context.Context, nodeID string, deadline time.Time) (bool, error)
	// AdvanceUpgrade atomically transitions nodeID's UpgradeState from any of
	// fromStates to `to`, returning true only if this call performed the
	// transition — so concurrent re-registers resolve exactly once. When
	// stampLastUpgrade is set (the transition to done), LastUpgrade is set to now
	// in the same update. A transition to failed clears UpgradeRebootAt, so a
	// later boot does not settle the failure as done.
	AdvanceUpgrade(ctx context.Context, nodeID string, fromStates []string, to string, stampLastUpgrade bool) (bool, error)
	// SettleAfterReboot resolves nodeID's pending upgrade or reset from the
	// boot state of a node that booted at bootedAt, when that is after the
	// reboot the pending command scheduled (UpgradeRebootAt, ResetRebootAt).
	// The check and the transition happen in one transaction, so a heartbeat
	// cannot settle an operation requested after the one it observed.
	SettleAfterReboot(ctx context.Context, nodeID, bootState string, bootedAt time.Time) error
	Delete(ctx context.Context, id string) error
}

// CommandStore manages the command queue.
type CommandStore interface {
	Create(ctx context.Context, cmd *NodeCommand) error
	// CreateBatch stores commands and marks the lifecycle
	// of their target nodes in a single transaction: a reset command sets the
	// node's reset state to pending and an upgrade command its upgrade state,
	// and stamps the request time.
	// Marking a reset pending also clears the node's reset progress stamp, so
	// the new reset's timeout window is measured from this request and never
	// inherits the previous reset's stamp.
	// A node whose upgrade is already pending, or whose reset is pending or
	// in-progress, keeps its state and request time, which belong to the
	// operation still waiting for the node to reboot.
	// Agents claim committed Pending rows on their own, so no row of the
	// batch may become visible before all of them and their lifecycle state
	// exist. Either everything is written or nothing is.
	//
	// Unlike Create, it keeps the ID and Phase the caller set, so the caller
	// can push the rows it built without reading them back. A command with an
	// empty ID gets a fresh one and an empty Phase becomes CommandPending.
	CreateBatch(ctx context.Context, cmds []*NodeCommand) error
	GetByID(ctx context.Context, id string) (*NodeCommand, error)
	GetPending(ctx context.Context, nodeID string) ([]*NodeCommand, error)
	MarkDelivered(ctx context.Context, ids []string) error
	// ClaimForDelivery atomically transitions a single command from Pending to
	// Delivered. It returns true only if this call performed the transition, so
	// exactly one of several concurrent claimants (a WS push and an agent poll,
	// or two concurrent polls) wins. A command that is missing or no longer
	// Pending yields (false, nil).
	ClaimForDelivery(ctx context.Context, id string) (bool, error)
	// UpdateStatus records a command's phase and result. A Failed reset or
	// upgrade also marks its node's pending lifecycle state failed in the same
	// write, unless another command of the same kind still owns that state: a
	// command that fails before the reboot never brings the node back with a
	// boot state the re-register resolvers could settle it from. It returns
	// ErrCommandNotFound when no command has the id, and ErrCommandTerminal
	// when the command already ended in another terminal phase.
	UpdateStatus(ctx context.Context, id string, phase string, result string) error
	// UpdateStatusForNode is UpdateStatus scoped to the commands of nodeID. It
	// returns ErrCommandNotFound when no command matches both the
	// command id and the owning node, so callers can fail closed (403/404)
	// instead of silently succeeding on a foreign or missing command, and
	// ErrCommandTerminal like UpdateStatus.
	UpdateStatusForNode(ctx context.Context, id string, nodeID string, phase string, result string) error
	ListByNode(ctx context.Context, nodeID string) ([]*NodeCommand, error)
	// ExpireBefore moves every command of nodeID that carries an ExpiresAt at
	// or before deadline, and has not reached a terminal phase, into
	// CommandExpired. GetPending already refuses to deliver such a command, so
	// without this transition the row is stranded in a phase nothing can ever
	// advance: DeleteTerminal does not collect it and the dashboard offers no
	// delete button for a Pending command.
	ExpireBefore(ctx context.Context, nodeID string, deadline time.Time) error
	Delete(ctx context.Context, id string) error
	// DeleteTerminal removes every command of nodeID that reached a terminal
	// phase: Completed, Failed or Expired.
	DeleteTerminal(ctx context.Context, nodeID string) error
}

// ArtifactRecord stores a build artifact and its metadata.
type ArtifactRecord struct {
	ID                      string               `json:"id" gorm:"primaryKey"`
	Name                    string               `json:"name,omitempty"`
	Saved                   bool                 `json:"saved,omitempty"`
	Phase                   string               `json:"phase"`
	Message                 string               `json:"message"`
	BaseImage               string               `json:"baseImage"`
	KairosVersion           string               `json:"kairosVersion"`
	Model                   string               `json:"model"`
	ISO                     bool                 `json:"iso"`
	CloudImage              bool                 `json:"cloudImage"`
	Netboot                 bool                 `json:"netboot"`
	FIPS                    bool                 `json:"fips"`
	TrustedBoot             bool                 `json:"trustedBoot"`
	Arch                    string               `json:"arch,omitempty"`
	Variant                 string               `json:"variant,omitempty"`
	AllowInsecureRegistries bool                 `json:"allow-insecure-registries" gorm:"column:insecure"`
	RawDisk                 bool                 `json:"rawDisk"`
	Tar                     bool                 `json:"tar"`
	GCE                     bool                 `json:"gce"`
	VHD                     bool                 `json:"vhd"`
	MAAS                    bool                 `json:"maas"`
	KairosInitImage         string               `json:"kairosInitImage,omitempty"`
	AutoInstall             bool                 `json:"autoInstall"`
	RegisterAuroraBoot      bool                 `json:"registerAuroraBoot"`
	Dockerfile              string               `json:"dockerfile,omitempty"`
	HadronBase              string               `json:"hadronBase,omitempty"`
	HadronFirmware          []string             `json:"hadronFirmware,omitempty" gorm:"serializer:json"`
	HadronLayers            []string             `json:"hadronLayers,omitempty" gorm:"serializer:json"`
	HadronExtra             string               `json:"hadronExtra,omitempty" gorm:"type:text"`
	CloudConfig             string               `json:"cloudConfig,omitempty" gorm:"type:text"`
	KubernetesDistro        string               `json:"kubernetesDistro,omitempty"`
	KubernetesVersion       string               `json:"kubernetesVersion,omitempty"`
	KubernetesEnabled       *bool                `json:"kubernetesEnabled,omitempty"`
	TargetGroupID           string               `json:"targetGroupId,omitempty"`
	ContainerImage          string               `json:"containerImage,omitempty"`
	OverlayID               string               `json:"overlayId,omitempty"`
	ArtifactFiles           []string             `json:"artifacts" gorm:"serializer:json"`
	ExtensionHierarchies    ExtensionHierarchies `gorm:"serializer:json" json:"extensionHierarchies"`
	Logs                    string               `json:"-" gorm:"type:text"`
	// UploadToken holds the sha256 hex digest of the per-build bearer the
	// operator backend's exporter Job uses to PUT /api/v1/artifacts/:id/upload/:file.
	// The plaintext token is minted by the handler on Create, injected into
	// the k8s Secret the exporter reads, and never written to the DB in
	// cleartext. Upload hashes the presented bearer and constant-time
	// compares it against this digest, so DB read access (backup, SQLite
	// file, SQLi elsewhere) does not surface live write tokens. The JSON
	// tag stays "-" so nothing here is serialized to clients regardless.
	UploadToken string    `json:"-"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// Extensions and ExtensionsCatalogs record the catalog extensions this
	// artifact was built with, so cloning it rebuilds with the same ones. An
	// empty catalog list means the build read extensions.DefaultCatalog,
	// which keeps a stored build following that default when it moves.
	Extensions         []string `json:"extensions,omitempty" gorm:"serializer:json"`
	ExtensionsCatalogs []string `json:"extensionsCatalogs,omitempty" gorm:"serializer:json"`
}

// ExtensionHierarchies records the SYSTEMD_{SYSEXT,CONFEXT}_HIERARCHIES paths
// declared at artifact build time so the Extensions UI can cross-check what
// scopes an OS image supports. /usr (sysext) and /etc (confext) are implicit
// and never stored in either slice.
type ExtensionHierarchies struct {
	Sysext  []string `json:"sysext"`
	Confext []string `json:"confext"`
}

// ExtensionRecord is one sysext or confext build managed by AuroraBoot.
// .raw output lives at <artifactsDir>/extensions/<ID>/<Name>.<Type>.raw.
type ExtensionRecord struct {
	ID      string `gorm:"primaryKey" json:"id"`
	Name    string `gorm:"index"      json:"name"`
	Type    string `json:"type"`  // "sysext" | "confext"
	Phase   string `json:"phase"` // Pending | Building | Ready | Error
	Message string `json:"message"`

	Arch    string `json:"arch"`
	Version string `json:"version"`

	SourceMode       string `json:"sourceMode"` // artifact | image | dockerfile
	SourceArtifactID string `json:"sourceArtifactId"`
	SourceImage      string `json:"sourceImage"`
	Dockerfile       string `gorm:"type:text" json:"dockerfile,omitempty"`
	ExtraSteps       string `gorm:"type:text" json:"extraSteps,omitempty"`

	SigningKeySetID string   `json:"signingKeySetId"`
	Hierarchies     []string `gorm:"serializer:json" json:"hierarchies"`
	ServiceReload   bool     `json:"serviceReload"`

	ContainerImage string `json:"containerImage"`
	RawFilename    string `json:"rawFilename"`

	// DownloadToken authorizes GET /extensions/:id/download/:filename for
	// this one extension and nothing else. It exists so the install command's
	// "source" URL -- which AuroraBoot pushes to every node in the selector,
	// stores in the commands table and renders in the UI's command preview --
	// does not have to carry the admin password. The scope is read-only, one
	// extension, and revoking it is a rebuild.
	//
	// Unlike ArtifactRecord.UploadToken this is the plaintext, not a digest:
	// the operator may open the Install dialog at any time after the build, so
	// the value has to be retrievable, and a digest cannot be. Both routes
	// that return it are admin-authenticated, so it reaches nobody who does
	// not already hold the admin password. Keep it that way: do not serialize
	// an ExtensionRecord on a node-facing route.
	DownloadToken string `json:"downloadToken,omitempty"`

	Logs string `gorm:"type:text" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// NodeExtensionRow is the per-node tracking that drives the Install dialog's
// pre-action diff and the node detail page's "Installed extensions" section.
// The agent's REST status callback writes/deletes rows on each successful
// install / disable / remove.
type NodeExtensionRow struct {
	NodeID      string    `gorm:"primaryKey" json:"nodeId"`
	Name        string    `gorm:"primaryKey" json:"name"`
	Type        string    `gorm:"primaryKey" json:"type"`      // sysext | confext
	BootState   string    `gorm:"primaryKey" json:"bootState"` // active | passive | recovery | common
	ExtensionID string    `                  json:"extensionId,omitempty"`
	Version     string    `                  json:"version"`
	InstalledAt time.Time `                  json:"installedAt"`
	UpdatedAt   time.Time `                  json:"updatedAt"`
}

// ArtifactExtensionBundle links an artifact to an extension that should ride
// with every upgrade to that artifact. Entries are by (ArtifactID,
// ExtensionName) -- the actual extension UUID is resolved at dispatch time so
// the bundle survives rebuilds of the named extension.
type ArtifactExtensionBundle struct {
	ArtifactID    string `gorm:"primaryKey" json:"artifactId"`
	ExtensionName string `gorm:"primaryKey" json:"extensionName"`
	ExtensionType string `                  json:"extensionType"` // sysext | confext
	PinnedVersion string `                  json:"pinnedVersion,omitempty"`
	Order         int    `                  json:"order"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Artifact phases.
const (
	ArtifactPending  = "Pending"
	ArtifactBuilding = "Building"
	ArtifactReady    = "Ready"
	ArtifactError    = "Error"
)

// ArtifactStore manages build artifact records.
type ArtifactStore interface {
	Create(ctx context.Context, rec *ArtifactRecord) error
	GetByID(ctx context.Context, id string) (*ArtifactRecord, error)
	List(ctx context.Context) ([]*ArtifactRecord, error)
	Update(ctx context.Context, rec *ArtifactRecord) error
	// UpdatePhaseMessage updates only the phase and message columns for the
	// record with the given id. Callers that only need to publish a phase
	// transition must prefer this over Update: Update rewrites every column
	// (including logs) from an in-memory copy and races with concurrent
	// AppendLog calls, silently dropping log lines that land between the
	// caller's GetByID and its Update.
	UpdatePhaseMessage(ctx context.Context, id, phase, message string) error
	// UpdateFiles updates only the artifact_files column for id. Upload uses
	// this instead of Update because a full-row Save from an Upload handler
	// would race watchCRPhase's phase writes: the handler snapshotted the
	// record at entry, then writes back a stale phase/message alongside the
	// new file list. Column-scoped writes converge cleanly.
	UpdateFiles(ctx context.Context, id string, files []string) error
	// UpdateNameSaved writes only the name and saved columns for id, and only
	// the ones that are non-nil. The PATCH handler uses this instead of Update
	// so its snapshotted phase, message, files and upload token never race
	// back over a concurrent write from the builder.
	UpdateNameSaved(ctx context.Context, id string, name *string, saved *bool) error
	// ClearUploadToken zeroes the upload_token column for id. watchCRPhase
	// calls this on the terminal transition so a leaked token cannot be
	// used to overwrite artifacts of a finished build.
	ClearUploadToken(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	DeleteByPhase(ctx context.Context, phase string) error
	GetLogs(ctx context.Context, id string) (string, error)
	AppendLog(ctx context.Context, id string, text string) error
}

// ExtensionStore manages sysext / confext extension records.
type ExtensionStore interface {
	Create(ctx context.Context, e *ExtensionRecord) error
	GetByID(ctx context.Context, id string) (*ExtensionRecord, error)
	List(ctx context.Context) ([]ExtensionRecord, error)
	Delete(ctx context.Context, id string) error
	FindLatestReadyByName(ctx context.Context, extType, name string) (*ExtensionRecord, error)
	FindByNameAndVersion(ctx context.Context, extType, name, version string) (*ExtensionRecord, error)
	AppendLog(ctx context.Context, id, chunk string) error
}

// ArtifactExtensionBundleStore manages the per-artifact list of bundled
// extensions that ride along with every upgrade to that artifact.
type ArtifactExtensionBundleStore interface {
	ListForArtifact(ctx context.Context, artifactID string) ([]ArtifactExtensionBundle, error)
	ReplaceForArtifact(ctx context.Context, artifactID string, entries []ArtifactExtensionBundle) error
	ArtifactsReferencingExtension(ctx context.Context, extensionName string) ([]string, error)
}

// NodeExtensionStore manages per-node installed extension tracking rows
// updated by the agent's status callback on install / disable / remove.
type NodeExtensionStore interface {
	Upsert(ctx context.Context, row *NodeExtensionRow) error
	ListForNode(ctx context.Context, nodeID string) ([]NodeExtensionRow, error)
	ListForExtensionByName(ctx context.Context, extType, name string) ([]NodeExtensionRow, error)
	DeleteByScope(ctx context.Context, nodeID, extType, name, bootState string) error
	DeleteByName(ctx context.Context, nodeID, extType, name string) error
}

// SecureBootKeySet tracks a named set of SecureBoot keys on the filesystem.
// KeysDir and TPMPCRKeyPath are server paths: the builders read them, and the
// API never returns them.
type SecureBootKeySet struct {
	ID               string    `json:"id" gorm:"primaryKey"`
	Name             string    `json:"name" gorm:"uniqueIndex"`
	KeysDir          string    `json:"-"`
	TPMPCRKeyPath    string    `json:"-"`
	SecureBootEnroll string    `json:"secureBootEnroll"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// SecureBootKeySetStore manages SecureBoot key sets.
type SecureBootKeySetStore interface {
	Create(ctx context.Context, ks *SecureBootKeySet) error
	GetByID(ctx context.Context, id string) (*SecureBootKeySet, error)
	GetByName(ctx context.Context, name string) (*SecureBootKeySet, error)
	List(ctx context.Context) ([]*SecureBootKeySet, error)
	Delete(ctx context.Context, id string) error
}

// BMCTarget stores saved RedFish/IPMI credentials for a baseboard management controller.
type BMCTarget struct {
	ID        string `json:"id" gorm:"primaryKey"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Vendor    string `json:"vendor"`
	Username  string `json:"username"`
	Password  string `json:"-"`
	VerifySSL bool   `json:"verifySSL"`
	// SystemID optionally pins the target ComputerSystem by its Redfish Id. Leave
	// empty when the BMC exposes exactly one system; it is required when the BMC
	// exposes more than one, or the deploy/inspect flow refuses to guess and fails
	// with the list of available system Ids. Mirrors the CLI's --system-id.
	SystemID string `json:"systemId,omitempty"`
	// ImageURL optionally overrides the global default image URL for this BMC: the
	// HTTP(S) URL this BMC pulls the ISO from (model a, operator-hosted). When set
	// it takes precedence over the global default but is still overridden by a
	// per-deploy imageUrl. SSRF-validated on write and again at deploy time.
	ImageURL string `json:"imageUrl,omitempty"`
	NodeID   string `json:"nodeId,omitempty" gorm:"index"`
	// EjectAfterInstall is the durable default eject policy for deployments made
	// against this BMC: when true, AuroraBoot ejects the virtual media (and steers
	// the next boot to disk) once the freshly-installed node phones home, breaking
	// the post-install install loop on BMCs that ignore the one-time boot override.
	// Default false (opt-in). A per-deploy request may override it.
	EjectAfterInstall bool `json:"ejectAfterInstall,omitempty"`
	// EjectPowerCycle, when true, makes the finalize/eject for this BMC power the
	// machine OFF before ejecting and back ON afterwards, instead of ejecting in
	// place on the running machine. It is the robust mode for BMCs/emulators (e.g.
	// sushy-tools on libvirt) that report a live eject but keep the running machine
	// booting the ISO. Default false (in-place eject), correct for hardware that
	// ejects live. It applies to every Finalize path for this BMC (auto and manual).
	EjectPowerCycle bool `json:"ejectPowerCycle,omitempty"`

	// --- Status cache (P4) ---
	//
	// These fields are server-owned denormalized snapshots of the last inspect /
	// reachability ping so the BMC page can render status without contacting the
	// BMC on load. Clients MUST NOT write them: CreateBMCTarget/UpdateBMCTarget
	// never copy them from the request body. They are populated only by the
	// inspect, status-ping and refresh-all handlers.
	LastStatus       string     `json:"lastStatus,omitempty"` // "", "reachable", "unreachable"
	LastError        string     `json:"lastError,omitempty"`
	LastInspectAt    *time.Time `json:"lastInspectAt,omitempty"`
	LastPingAt       *time.Time `json:"lastPingAt,omitempty"`
	LastModel        string     `json:"lastModel,omitempty"`
	LastManufacturer string     `json:"lastManufacturer,omitempty"`
	LastSerial       string     `json:"lastSerial,omitempty"`
	LastMemoryGiB    int        `json:"lastMemoryGiB,omitempty"`
	LastCPUCount     int        `json:"lastCpuCount,omitempty"`
	LastFeatures     []string   `json:"lastFeatures,omitempty" gorm:"serializer:json"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// BMCTargetStore manages BMC target records.
type BMCTargetStore interface {
	Create(ctx context.Context, target *BMCTarget) error
	GetByID(ctx context.Context, id string) (*BMCTarget, error)
	List(ctx context.Context) ([]*BMCTarget, error)
	Update(ctx context.Context, target *BMCTarget) error
	Delete(ctx context.Context, id string) error
}

// Deployment tracks a deployment operation.
type Deployment struct {
	ID          string     `json:"id" gorm:"primaryKey"`
	ArtifactID  string     `json:"artifactId" gorm:"index"`
	Method      string     `json:"method"`
	Status      string     `json:"status"`
	Message     string     `json:"message"`
	BMCTargetID string     `json:"bmcTargetId,omitempty"`
	Progress    int        `json:"progress"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`

	// --- Eject / finalize (P5) ---
	//
	// NodeID links this deployment to the node that phoned home from it, so an
	// auto eject-on-phone-home can correlate node -> deployment. It is set lazily
	// when the finalize path picks this deployment for a node.
	NodeID string `json:"nodeId,omitempty" gorm:"index"`
	// EjectPolicy records the resolved policy at deploy completion: "" / "off"
	// (no eject), "on-phone-home" (auto eject when the node registers/heartbeats),
	// or "manual" (operator-only).
	EjectPolicy string `json:"ejectPolicy,omitempty"`
	// EjectState is the CAS idempotency key for the finalize lifecycle:
	// "" (not applicable) -> "pending" -> "ejecting" -> "ejected" | "eject-failed".
	// The pending->ejecting transition is a compare-and-set so exactly one finalize
	// attempt (auto or manual) wins.
	EjectState string `json:"ejectState,omitempty"`
	// EjectError carries a scrubbed failure reason when EjectState == "eject-failed".
	EjectError string `json:"ejectError,omitempty"`
	// EjectedAt is set when the media was successfully ejected.
	EjectedAt *time.Time `json:"ejectedAt,omitempty"`
}

// Deployment statuses.
const (
	DeployActive    = "Active"
	DeployCompleted = "Completed"
	DeployFailed    = "Failed"
)

// Eject policies (Deployment.EjectPolicy).
const (
	EjectPolicyOff         = "off"
	EjectPolicyOnPhoneHome = "on-phone-home"
	EjectPolicyManual      = "manual"
)

// Eject states (Deployment.EjectState). EjectStatePending is the entry point the
// deploy path sets when the policy is not off; the rest are driven by Finalize.
const (
	EjectStatePending  = "pending"
	EjectStateEjecting = "ejecting"
	EjectStateEjected  = "ejected"
	EjectStateFailed   = "eject-failed"
)

// DeploymentStore manages deployment records.
type DeploymentStore interface {
	Create(ctx context.Context, dep *Deployment) error
	GetByID(ctx context.Context, id string) (*Deployment, error)
	List(ctx context.Context) ([]*Deployment, error)
	ListByArtifact(ctx context.Context, artifactID string) ([]*Deployment, error)
	Update(ctx context.Context, dep *Deployment) error
	Delete(ctx context.Context, id string) error
	// CASEjectState atomically transitions a deployment's EjectState from `from`
	// to `to`, returning true only if this call performed the transition. It is the
	// idempotency guard for finalize: when an auto eject-on-phone-home and a manual
	// finalize (or two concurrent phone-homes) target the same deployment, exactly
	// one observes the still-`from` row and wins; the losers see (false, nil). A
	// missing deployment or a mismatched current state yields (false, nil).
	CASEjectState(ctx context.Context, id, from, to string) (bool, error)
}

// Setting is a single opaque key/value row backing runtime-configurable server
// settings (e.g. the image-source defaults). Values are stored as plain strings;
// the consuming handler owns typing and any SSRF/validation of the value. Keys are
// namespaced (e.g. "imageSource.defaultImageURL").
type Setting struct {
	Key       string    `json:"key" gorm:"primaryKey"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SettingsStore manages opaque key/value settings.
type SettingsStore interface {
	// Get returns the value for key. found reports whether the key exists; a
	// missing key is (", false, nil), not an error.
	Get(ctx context.Context, key string) (value string, found bool, err error)
	// Set upserts key to value.
	Set(ctx context.Context, key, value string) error
	// GetAll returns every setting as a key→value map.
	GetAll(ctx context.Context) (map[string]string, error)
}
