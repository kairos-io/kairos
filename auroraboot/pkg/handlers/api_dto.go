package handlers

// This file defines the exported request/response types that swag
// scans when generating the OpenAPI spec. The actual handlers continue
// to use whatever internal shapes they like — these DTOs exist purely
// so the generated docs can reference concrete, named JSON schemas
// instead of `any` or `map[string]string`.
//
// Keep these in sync with the handler implementations: if you change a
// request body shape, update the matching DTO here and re-run
// `make openapi` so the generated spec stays accurate.

import (
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// --- Error envelope ---

// APIError is the standard error body returned when a handler rejects a
// request. Code is an optional stable, machine-readable discriminator (e.g.
// "NoCapacity") that lets a programmatic client branch on the specific
// condition without parsing the human-facing Error text.
type APIError struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code,omitempty"`
}

// APIBusyNodesError is the 409 body returned when a command or batch names
// nodes that are in a batch that has not finished with them.
type APIBusyNodesError struct {
	Error   string   `json:"error"`
	NodeIDs []string `json:"nodeIDs"`
}

// --- Node claim (kairos-io/kairos#4252) ---

// APIClaimRequest is the JSON body of POST /api/v1/groups/:id/claim. ClaimKey is
// an opaque, caller-owned identifier (e.g. a CAPI machine's UID): re-issuing a
// claim with the same key returns the same node, which is what makes a
// level-triggered reconcile safe.
type APIClaimRequest struct {
	ClaimKey string `json:"claimKey" example:"machine-abc123"`
}

// APIReleaseRequest is the JSON body of POST /api/v1/nodes/:nodeID/release. The
// claim is only cleared if it is currently held by ClaimKey.
type APIReleaseRequest struct {
	ClaimKey string `json:"claimKey" example:"machine-abc123"`
}

// APIReleaseResponse reports whether the release cleared a claim. Released is
// false (with 200) when the node was already unclaimed — release is idempotent.
type APIReleaseResponse struct {
	Released bool `json:"released"`
}

// ClaimErrorCodeNoCapacity is the APIError.Code returned (with HTTP 409) when a
// claim finds no unclaimed node in the group.
const ClaimErrorCodeNoCapacity = "NoCapacity"

// ClaimErrorCodeClaimMismatch is the APIError.Code returned (with HTTP 409) when
// a release names a node that is claimed by a different key.
const ClaimErrorCodeClaimMismatch = "ClaimMismatch"

// --- Nodes ---

// APIRegisterRequest is the JSON body of POST /api/v1/nodes/register.
type APIRegisterRequest struct {
	RegistrationToken string `json:"registrationToken" example:"74a7e7452f9da2fd7739557f027bca0f"`
	MachineID         string `json:"machineID" example:"a1b2c3d4e5f6"`
	Hostname          string `json:"hostname" example:"kairos-node-01"`
	AgentVersion      string `json:"agentVersion" example:"v2.27.0"`
	// Addresses are the node's reported network addresses (optional, multi-NIC).
	// Stored exactly as received; interface filtering is the agent's job.
	Addresses []store.NodeAddress `json:"addresses,omitempty"`
	// BootState is the node's reported boot state (optional): one of
	// active | passive | recovery | autoreset (unknown values pass through).
	BootState string `json:"bootState,omitempty" example:"active"`
	// Group is the ID or name of the group a new node joins (optional); an ID
	// match wins over a name match. The holder of the registration token
	// chooses it for a new node, so the registration token decides group
	// placement. An unknown group leaves the node ungrouped. Ignored when the
	// node registers again.
	Group string `json:"group,omitempty" example:"edge-sites"`
	// Labels are a new node's initial labels (optional). The holder of the
	// registration token chooses them for a new node. Ignored when the node
	// registers again.
	Labels map[string]string `json:"labels,omitempty"`
}

// APIRegisterResponse is the JSON body returned by POST /api/v1/nodes/register.
type APIRegisterResponse struct {
	ID     string `json:"id" example:"c8a4fb46-1836-4c70-97c8-29490ad110bc"`
	APIKey string `json:"apiKey" example:"3db2c1e4f5a6b7c8d9e0f1a2b3c4d5e6"`
}

// APIHeartbeatRequest is the JSON body of POST /api/v1/nodes/:nodeID/heartbeat.
type APIHeartbeatRequest struct {
	AgentVersion string            `json:"agentVersion" example:"v2.27.0"`
	OSRelease    map[string]string `json:"osRelease"`
	Labels       map[string]string `json:"labels"`
	// Addresses are the node's current network addresses (optional, multi-NIC).
	// When present they replace the stored list (e.g. after a DHCP change); when
	// omitted the stored list is preserved.
	Addresses []store.NodeAddress `json:"addresses,omitempty"`
	// BootState is the node's current boot state (optional): one of
	// active | passive | recovery | autoreset (unknown values pass through).
	BootState string `json:"bootState,omitempty" example:"active"`
	// Hostname is the node's current hostname (optional). Reported on every
	// heartbeat, not only at registration, so a hostname that cloud-init applies
	// after phone-home has registered, or a later rename, reaches the record. When
	// omitted the stored hostname is preserved.
	Hostname string `json:"hostname,omitempty" example:"kairos-node-01"`
	// Metrics is an optional resource sample. The server keeps recent samples in
	// memory. A sample that does not decode is ignored and does not fail the
	// heartbeat.
	Metrics *store.NodeMetrics `json:"metrics,omitempty"`
}

// APINodeMetricsResponse is the JSON body returned by
// GET /api/v1/nodes/:nodeID/metrics. Latest is null when the node has no
// samples. Samples are oldest first and never null.
type APINodeMetricsResponse struct {
	Latest  *store.NodeMetrics  `json:"latest"`
	Samples []store.NodeMetrics `json:"samples"`
}

// APISetLabelsRequest is the JSON body of PUT /api/v1/nodes/:nodeID/labels.
type APISetLabelsRequest struct {
	Labels map[string]string `json:"labels"`
}

// APISetGroupRequest is the JSON body of PUT /api/v1/nodes/:nodeID/group.
type APISetGroupRequest struct {
	// GroupID names the target group; null moves the node out of any group.
	GroupID *string `json:"groupID" example:"b1db4f35-6195-410a-94f2-09e66e0a6a83" extensions:"x-nullable"`
}

// --- Groups ---

// APICreateGroupRequest is the JSON body of POST /api/v1/groups.
type APICreateGroupRequest struct {
	Name        string `json:"name" example:"production"`
	Description string `json:"description" example:"Production fleet nodes"`
}

// APIUpdateGroupRequest is the JSON body of PUT /api/v1/groups/:id.
type APIUpdateGroupRequest struct {
	// Name replaces the group name; an empty or omitted name keeps the current one.
	Name string `json:"name"`
	// Description replaces the group description; an empty string clears it and an omitted field keeps the current one.
	Description *string `json:"description"`
}

// --- Commands ---

// APICreateCommandRequest is the JSON body of
// POST /api/v1/nodes/:nodeID/commands.
type APICreateCommandRequest struct {
	Command string            `json:"command" example:"upgrade" enums:"upgrade,upgrade-recovery,reset,apply-cloud-config,reboot,exec"`
	Args    map[string]string `json:"args"`
}

// APIRolloutOptions are the batch settings of the group and bulk command bodies.
type APIRolloutOptions struct {
	// Concurrency caps how many nodes run at once. 0 runs every node at once.
	Concurrency int `json:"concurrency,omitempty"`
	// StopOnFailure stops the batch when a node fails. Defaults to true.
	StopOnFailure *bool `json:"stopOnFailure,omitempty"`
	// RebootTimeoutSeconds is how long a node that reboots may take to come
	// back. Defaults to 600, at most 604800 (7 days).
	RebootTimeoutSeconds int `json:"rebootTimeoutSeconds,omitempty"`
}

// APIGroupCommandRequest is the JSON body of POST /api/v1/groups/:id/commands.
type APIGroupCommandRequest struct {
	Command string            `json:"command" example:"upgrade"`
	Args    map[string]string `json:"args"`
	APIRolloutOptions
}

// APIBulkCommandRequest is the JSON body of POST /api/v1/nodes/commands.
type APIBulkCommandRequest struct {
	Selector store.CommandSelector `json:"selector"`
	Command  string                `json:"command" example:"upgrade"`
	Args     map[string]string     `json:"args"`
	APIRolloutOptions
}

// APIUpdateCommandStatusRequest is the JSON body of
// PUT /api/v1/nodes/:nodeID/commands/:commandID/status.
type APIUpdateCommandStatusRequest struct {
	Phase  string `json:"phase" enums:"Pending,Delivered,Running,Completed,Failed,Expired"`
	Result string `json:"result"`
}

// --- Artifacts ---

// APICreateArtifactRequest is the JSON body of POST /api/v1/artifacts.
// Mirrors the shape the frontend sends; see internal/handlers/artifacts.go
// for the authoritative parser.
type APICreateArtifactRequest struct {
	Name                    string                  `json:"name"`
	BaseImage               string                  `json:"baseImage"`
	KairosVersion           string                  `json:"kairosVersion" example:"v1.0"`
	Model                   string                  `json:"model" example:"generic"`
	Arch                    string                  `json:"arch" example:"amd64" enums:"amd64,arm64"`
	Variant                 string                  `json:"variant" example:"core" enums:"core,standard"`
	KubernetesDistro        string                  `json:"kubernetesDistro" enums:"k3s,k0s"`
	KubernetesVersion       string                  `json:"kubernetesVersion"`
	KubernetesEnabled       *bool                   `json:"kubernetesEnabled"`
	AllowInsecureRegistries bool                    `json:"allow-insecure-registries" example:"false"`
	Dockerfile              string                  `json:"dockerfile"`
	HadronBase              string                  `json:"hadronBase"`
	HadronFirmware          []string                `json:"hadronFirmware"`
	HadronLayers            []string                `json:"hadronLayers"`
	HadronExtra             string                  `json:"hadronExtra"`
	OverlayID               string                  `json:"overlayId" format:"uuid"` // ID returned by POST /api/v1/artifacts/upload-overlay
	KairosInitImage         string                  `json:"kairosInitImage"`
	Outputs                 APIArtifactOutputs      `json:"outputs"`
	Signing                 APIArtifactSigning      `json:"signing"`
	Provisioning            APIArtifactProvisioning `json:"provisioning"`
	CloudConfig             string                  `json:"cloudConfig"`
	// Extensions are catalog extension names (name or name@version) to place
	// in the built ISO. ExtensionsCatalogs replaces the default catalog they
	// resolve against; each one must be an http or https URL.
	Extensions         []string `json:"extensions"`
	ExtensionsCatalogs []string `json:"extensionsCatalogs"`
}

// APIArtifactOutputs toggles the build's output formats.
type APIArtifactOutputs struct {
	// The installer ISO. With trustedBoot it is the signed UKI ISO, which
	// needs signing.ukiKeySetId.
	ISO        bool `json:"iso"`
	CloudImage bool `json:"cloudImage"`
	// Not available with trustedBoot, like rawDisk, cloudImage, gce, vhd and
	// maas: a Trusted Boot image has no GRUB and no separate initrd.
	Netboot     bool `json:"netboot"`
	RawDisk     bool `json:"rawDisk"`
	Tar         bool `json:"tar"`
	GCE         bool `json:"gce"`
	VHD         bool `json:"vhd"`
	MAAS        bool `json:"maas"`
	FIPS        bool `json:"fips"`
	TrustedBoot bool `json:"trustedBoot"`
}

// APIArtifactSigning holds SecureBoot signing options for UKI builds.
type APIArtifactSigning struct {
	UKIKeySetID         string `json:"ukiKeySetId"`
	UKISecureBootEnroll string `json:"ukiSecureBootEnroll" enums:"off,manual,if-safe,force"`
}

// APIArtifactProvisioning holds cloud-config injection options.
type APIArtifactProvisioning struct {
	AutoInstall        bool   `json:"autoInstall"`
	RegisterAuroraBoot bool   `json:"registerAuroraBoot"`
	TargetGroupID      string `json:"targetGroupId"`
	UserMode           string `json:"userMode" enums:"default,custom,none"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	SSHKeys            string `json:"sshKeys"`
	// AllowedCommands is the explicit list emitted under phonehome.allowed_commands.
	// Nil means "use AuroraBoot's safe default set". An empty slice means deny-all
	// (observe-only node) — the UI warns when the operator picks this.
	AllowedCommands []string `json:"allowedCommands"`
}

// APIUpdateArtifactRequest is the JSON body of PATCH /api/v1/artifacts/:id.
type APIUpdateArtifactRequest struct {
	Name  string `json:"name"`
	Saved *bool  `json:"saved"`
}

// --- SecureBoot keys ---

// APIGenerateKeySetRequest is the JSON body of
// POST /api/v1/secureboot-keys/generate.
type APIGenerateKeySetRequest struct {
	Name             string `json:"name" example:"production"`
	SecureBootEnroll string `json:"secureBootEnroll" enums:"off,manual,if-safe,force"`
}

// --- Settings ---

// APIRegistrationTokenResponse wraps the registration token for the
// settings endpoints.
type APIRegistrationTokenResponse struct {
	RegistrationToken string `json:"registrationToken"`
}

// --- System ---

// APISystemBuilder is the JSON body returned by GET /api/v1/system/builder.
// Reports which build backend is active and enough context for the UI to
// render a "Builds via cluster X" badge and decide whether to expose the
// download UI. `cluster` and `namespace` are omitted for the local backend.
type APISystemBuilder struct {
	Backend           string `json:"backend" example:"operator" enums:"local,operator"`
	Cluster           string `json:"cluster,omitempty" example:"https://kind.example"`
	Namespace         string `json:"namespace,omitempty" example:"kairos-builds"`
	DownloadSupported bool   `json:"downloadSupported"`
}

// Verify time is imported — otherwise unused-import error.
var _ = time.Time{}
