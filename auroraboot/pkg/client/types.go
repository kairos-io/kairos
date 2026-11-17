package client

import "time"

// These types describe the JSON wire format of the AuroraBoot REST API.
// They intentionally live in pkg/client rather than being imported
// from internal/store so that third-party consumers can pull this
// package in without dragging the private GORM layer with them.
//
// If you change a wire shape server-side, update the matching DTO
// here and run `make openapi` to refresh the generated spec.

// NodePhase is the lifecycle state of a managed node.
type NodePhase string

const (
	NodePhasePending    NodePhase = "Pending"
	NodePhaseRegistered NodePhase = "Registered"
	NodePhaseOnline     NodePhase = "Online"
	NodePhaseOffline    NodePhase = "Offline"
)

// Node describes a Kairos node known to AuroraBoot. GroupID is nil for a node
// outside any group.
type Node struct {
	ID            string            `json:"id"`
	MachineID     string            `json:"machineID"`
	Hostname      string            `json:"hostname"`
	GroupID       *string           `json:"groupID,omitempty"`
	Group         *Group            `json:"group,omitempty"`
	Phase         NodePhase         `json:"phase"`
	LastHeartbeat *time.Time        `json:"lastHeartbeat,omitempty"`
	AgentVersion  string            `json:"agentVersion"`
	OSRelease     map[string]string `json:"osRelease,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	// ClaimKey is the opaque key that has claimed this node (nil when unclaimed);
	// ClaimedAt is when it was claimed. See GroupsService.Claim.
	ClaimKey  *string    `json:"claimKey,omitempty"`
	ClaimedAt *time.Time `json:"claimedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// Group is a logical bucket of nodes (environment, cluster, role...).
type Group struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	NodeCount   int       `json:"node_count,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// NodeRegisterRequest is the body of POST /api/v1/nodes/register.
type NodeRegisterRequest struct {
	RegistrationToken string `json:"registrationToken"`
	MachineID         string `json:"machineID"`
	Hostname          string `json:"hostname,omitempty"`
	AgentVersion      string `json:"agentVersion,omitempty"`
	// Group is the name of the group a new node joins; an unknown name leaves
	// it ungrouped.
	Group string `json:"group,omitempty"`
	// Labels are a new node's initial labels.
	Labels map[string]string `json:"labels,omitempty"`
}

// NodeRegisterResponse is returned by POST /api/v1/nodes/register.
type NodeRegisterResponse struct {
	ID     string `json:"id"`
	APIKey string `json:"apiKey"`
}

// NodeHeartbeatRequest is the body of POST /api/v1/nodes/:nodeID/heartbeat.
type NodeHeartbeatRequest struct {
	AgentVersion string            `json:"agentVersion,omitempty"`
	OSRelease    map[string]string `json:"osRelease,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// NodeListOptions filters the GET /api/v1/nodes response.
type NodeListOptions struct {
	// GroupID filters by exact group ID.
	GroupID string
	// Label filters by a single "key:value" pair. The server supports
	// exactly one label filter per request.
	Label string
}

// CommandPhase is the lifecycle state of a queued remote command.
type CommandPhase string

const (
	CommandPhasePending   CommandPhase = "Pending"
	CommandPhaseDelivered CommandPhase = "Delivered"
	CommandPhaseRunning   CommandPhase = "Running"
	CommandPhaseCompleted CommandPhase = "Completed"
	CommandPhaseFailed    CommandPhase = "Failed"
	CommandPhaseExpired   CommandPhase = "Expired"
	// CommandPhaseCanceled is a command a fail-fast batch stopped before it was
	// ever delivered, so the node ran nothing.
	CommandPhaseCanceled CommandPhase = "Canceled"
)

// NodeCommand describes a queued remote operation.
type NodeCommand struct {
	ID            string            `json:"id"`
	ManagedNodeID string            `json:"managedNodeID"`
	Command       string            `json:"command"`
	Args          map[string]string `json:"args,omitempty"`
	Phase         CommandPhase      `json:"phase"`
	Result        string            `json:"result,omitempty"`
	ExpiresAt     *time.Time        `json:"expiresAt,omitempty"`
	DeliveredAt   *time.Time        `json:"deliveredAt,omitempty"`
	CompletedAt   *time.Time        `json:"completedAt,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	// BatchID names the batch this command came from. It is empty on a
	// command addressed to one node.
	BatchID string `json:"batchID,omitempty"`
}

// RolloutOptions are the settings of the batch a group or bulk command starts.
type RolloutOptions struct {
	// Concurrency caps how many nodes run at once. 0 runs every node at once.
	Concurrency int `json:"concurrency,omitempty"`
	// StopOnFailure stops the batch when a node fails. nil means true.
	StopOnFailure *bool `json:"stopOnFailure,omitempty"`
	// RebootTimeoutSeconds is how long a node that reboots may take to come
	// back. 0 uses the server default; the server rejects more than 604800
	// (7 days).
	RebootTimeoutSeconds int `json:"rebootTimeoutSeconds,omitempty"`
}

// BatchNode is one node's row in a Batch.
type BatchNode struct {
	BatchID        string     `json:"batchID"`
	NodeID         string     `json:"nodeID"`
	Position       int        `json:"position"`
	Hostname       string     `json:"hostname"`
	CommandID      *string    `json:"commandID"`
	State          string     `json:"state"`
	Message        string     `json:"message"`
	StartedAt      *time.Time `json:"startedAt"`
	RebootingSince *time.Time `json:"rebootingSince"`
	Deadline       *time.Time `json:"deadline"`
	FinishedAt     *time.Time `json:"finishedAt"`
}

// Batch is a command rolled out over several nodes a few at a time. Nodes is
// filled by Get, Stop and the calls that start a batch, not by List.
type Batch struct {
	ID                   string            `json:"id"`
	Command              string            `json:"command"`
	Args                 map[string]string `json:"args"`
	Selection            string            `json:"selection"`
	GroupID              *string           `json:"groupID"`
	Concurrency          int               `json:"concurrency"`
	StopOnFailure        bool              `json:"stopOnFailure"`
	RebootTimeoutSeconds int               `json:"rebootTimeoutSeconds"`
	Phase                string            `json:"phase"`
	CreatedAt            time.Time         `json:"createdAt"`
	FinishedAt           *time.Time        `json:"finishedAt"`
	StoppedAt            *time.Time        `json:"stoppedAt"`
	StopReason           string            `json:"stopReason"`
	StoppedByNodeID      *string           `json:"stoppedByNodeID"`
	Counts               map[string]int    `json:"counts"`
	// Hostnames holds the hostnames of the batch's first nodes. Only lists fill it.
	Hostnames []string    `json:"hostnames,omitempty"`
	Nodes     []BatchNode `json:"nodes,omitempty"`
}

// BatchList is one page of batches and the total number that match.
type BatchList struct {
	Batches []Batch `json:"batches"`
	Total   int     `json:"total"`
}

// BatchListOptions filters and pages Batches.List. Empty fields are not sent.
type BatchListOptions struct {
	GroupID string
	NodeID  string
	Limit   int
	Offset  int
}

// CreateCommandRequest is the body of POST /api/v1/nodes/:nodeID/commands.
type CreateCommandRequest struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
}

// GroupCommandRequest is the body of POST /api/v1/groups/:id/commands. The
// group is named in the path, so unlike BulkCommandRequest it has no selector.
type GroupCommandRequest struct {
	Command string            `json:"command"`
	Args    map[string]string `json:"args,omitempty"`
	RolloutOptions
}

// CommandSelector targets nodes for a bulk command operation.
// Fields are AND'ed; NodeIDs if set takes precedence over Group/Labels.
type CommandSelector struct {
	GroupID string            `json:"groupID,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	NodeIDs []string          `json:"nodeIDs,omitempty"`
}

// BulkCommandRequest is the body of POST /api/v1/nodes/commands.
type BulkCommandRequest struct {
	Selector CommandSelector   `json:"selector"`
	Command  string            `json:"command"`
	Args     map[string]string `json:"args,omitempty"`
	RolloutOptions
}

// UpdateCommandStatusRequest is the body used by agents to report
// command progress.
type UpdateCommandStatusRequest struct {
	Phase  CommandPhase `json:"phase"`
	Result string       `json:"result,omitempty"`
}

// ArtifactPhase is the lifecycle state of a build.
type ArtifactPhase string

const (
	ArtifactPhasePending  ArtifactPhase = "Pending"
	ArtifactPhaseBuilding ArtifactPhase = "Building"
	ArtifactPhaseReady    ArtifactPhase = "Ready"
	ArtifactPhaseError    ArtifactPhase = "Error"
)

// Artifact is the stored representation of a build. Note that
// `KairosVersion` is the user-facing artifact version (baked into the
// image and used for upgrade tracking), not the Kairos framework
// version of the base image.
type Artifact struct {
	ID                      string        `json:"id"`
	Name                    string        `json:"name,omitempty"`
	Saved                   bool          `json:"saved,omitempty"`
	Phase                   ArtifactPhase `json:"phase"`
	Message                 string        `json:"message,omitempty"`
	BaseImage               string        `json:"baseImage"`
	KairosVersion           string        `json:"kairosVersion"`
	Model                   string        `json:"model"`
	Arch                    string        `json:"arch"`
	Variant                 string        `json:"variant"`
	AllowInsecureRegistries bool          `json:"allow-insecure-registries"`
	KubernetesDistro        string        `json:"kubernetesDistro,omitempty"`
	KubernetesEnabled       *bool         `json:"kubernetesEnabled,omitempty"`
	ISO                     bool          `json:"iso"`
	CloudImage              bool          `json:"cloudImage"`
	Netboot                 bool          `json:"netboot"`
	RawDisk                 bool          `json:"rawDisk"`
	Tar                     bool          `json:"tar"`
	GCE                     bool          `json:"gce"`
	VHD                     bool          `json:"vhd"`
	MAAS                    bool          `json:"maas"`
	FIPS                    bool          `json:"fips"`
	TrustedBoot             bool          `json:"trustedBoot"`
	AutoInstall             bool          `json:"autoInstall"`
	RegisterAuroraBoot      bool          `json:"registerAuroraBoot"`
	Dockerfile              string        `json:"dockerfile,omitempty"`
	HadronBase              string        `json:"hadronBase,omitempty"`
	HadronFirmware          []string      `json:"hadronFirmware,omitempty"`
	HadronLayers            []string      `json:"hadronLayers,omitempty"`
	HadronExtra             string        `json:"hadronExtra,omitempty"`
	CloudConfig             string        `json:"cloudConfig,omitempty"`
	OverlayID               string        `json:"overlayId,omitempty"`
	TargetGroupID           string        `json:"targetGroupId,omitempty"`
	ContainerImage          string        `json:"containerImage,omitempty"`
	Artifacts               []string      `json:"artifacts,omitempty"`
	CreatedAt               time.Time     `json:"createdAt"`
	UpdatedAt               time.Time     `json:"updatedAt"`
}

// CreateArtifactRequest is the body of POST /api/v1/artifacts.
// This is a large struct; most fields are optional and reasonable
// defaults are applied server-side. Mirrors internal handler DTO.
//
// OverlayID references files uploaded with ArtifactsService.UploadOverlay;
// they are copied on top of the image rootfs.
type CreateArtifactRequest struct {
	Name                    string                 `json:"name,omitempty"`
	BaseImage               string                 `json:"baseImage,omitempty"`
	KairosVersion           string                 `json:"kairosVersion,omitempty"`
	Model                   string                 `json:"model,omitempty"`
	Arch                    string                 `json:"arch,omitempty"`
	Variant                 string                 `json:"variant,omitempty"`
	KubernetesDistro        string                 `json:"kubernetesDistro,omitempty"`
	KubernetesVersion       string                 `json:"kubernetesVersion,omitempty"`
	KubernetesEnabled       *bool                  `json:"kubernetesEnabled,omitempty"`
	AllowInsecureRegistries bool                   `json:"allow-insecure-registries,omitempty"`
	Dockerfile              string                 `json:"dockerfile,omitempty"`
	HadronBase              string                 `json:"hadronBase,omitempty"`
	HadronFirmware          []string               `json:"hadronFirmware,omitempty"`
	HadronLayers            []string               `json:"hadronLayers,omitempty"`
	HadronExtra             string                 `json:"hadronExtra,omitempty"`
	OverlayID               string                 `json:"overlayId,omitempty"`
	KairosInitImage         string                 `json:"kairosInitImage,omitempty"`
	Outputs                 ArtifactOutputs        `json:"outputs"`
	Signing                 ArtifactSigning        `json:"signing"`
	Provisioning            ArtifactProvisioning   `json:"provisioning"`
	CloudConfig             string                 `json:"cloudConfig,omitempty"`
	Extra                   map[string]interface{} `json:"-"` // reserved for future fields
}

// ArtifactOutputs toggles build output formats.
//
// There is one ISO option and TrustedBoot decides which ISO it is: the GRUB
// installer in Standard mode, the signed UKI installer in Trusted Boot mode.
// A Trusted Boot image has no GRUB, so netboot and the disk images are
// refused on such a build.
type ArtifactOutputs struct {
	ISO         bool `json:"iso,omitempty"`
	CloudImage  bool `json:"cloudImage,omitempty"`
	Netboot     bool `json:"netboot,omitempty"`
	RawDisk     bool `json:"rawDisk,omitempty"`
	Tar         bool `json:"tar,omitempty"`
	GCE         bool `json:"gce,omitempty"`
	VHD         bool `json:"vhd,omitempty"`
	MAAS        bool `json:"maas,omitempty"`
	FIPS        bool `json:"fips,omitempty"`
	TrustedBoot bool `json:"trustedBoot,omitempty"`
}

// ArtifactSigning describes UKI SecureBoot signing options. Builds are
// signed with a key set stored on the server, selected by UKIKeySetID
// (see SecureBootService).
type ArtifactSigning struct {
	UKIKeySetID         string `json:"ukiKeySetId,omitempty"`
	UKISecureBootEnroll string `json:"ukiSecureBootEnroll,omitempty"`
}

// ArtifactProvisioning describes cloud-config injection options.
type ArtifactProvisioning struct {
	AutoInstall        bool   `json:"autoInstall"`
	RegisterAuroraBoot bool   `json:"registerAuroraBoot"`
	TargetGroupID      string `json:"targetGroupId,omitempty"`
	UserMode           string `json:"userMode,omitempty"`
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	SSHKeys            string `json:"sshKeys,omitempty"`
}

// UpdateArtifactRequest is the body of PATCH /api/v1/artifacts/:id.
type UpdateArtifactRequest struct {
	Name  string `json:"name,omitempty"`
	Saved *bool  `json:"saved,omitempty"`
}

// SecureBootKeySet is a generated or imported UKI signing key set.
type SecureBootKeySet struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	SecureBootEnroll string    `json:"secureBootEnroll"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	// HasTPMPCRKey reports whether the set holds a TPM PCR signing key.
	HasTPMPCRKey bool `json:"hasTpmPcrKey"`
}

// GenerateKeySetRequest is the body of POST /api/v1/secureboot-keys/generate.
type GenerateKeySetRequest struct {
	Name             string `json:"name"`
	SecureBootEnroll string `json:"secureBootEnroll,omitempty"`
}

// RegistrationTokenResponse wraps the current registration token.
type RegistrationTokenResponse struct {
	RegistrationToken string `json:"registrationToken"`
}
