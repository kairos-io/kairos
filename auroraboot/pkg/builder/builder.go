package builder

import (
	"context"
	"errors"
)

// ErrInvalidBuildOptions marks a build failure caused by invalid admin-supplied
// build inputs (e.g. a Model/KairosVersion/KubernetesVersion that fails
// validation) rather than a server-side fault. Handlers can use errors.Is to
// map such failures to a 400 Bad Request instead of a 500.
var ErrInvalidBuildOptions = errors.New("invalid build options")

// ErrNotSupported marks a builder-backend refusal because the caller asked for
// something the backend cannot do (e.g. an unimplemented operation on a stub,
// or an output format a specific backend does not produce). Handlers map this
// to 501 Not Implemented rather than 500 or 404, so the UI can distinguish
// "your request is fine, this backend just cannot serve it" from real faults.
var ErrNotSupported = errors.New("operation not supported by builder")

// ImageSource describes the base image and its properties.
type ImageSource struct {
	BaseImage         string
	KairosVersion     string
	Model             string
	Arch              string // "amd64" or "arm64"
	Variant           string // "core" or "standard"
	KubernetesDistro  string
	KubernetesVersion string
	// AllowInsecureRegistries allows pulling the base image from a registry
	// served over plain HTTP or presenting an untrusted/self-signed TLS
	// certificate.
	AllowInsecureRegistries bool
}

// OutputOptions selects which artifact formats to produce.
type OutputOptions struct {
	ISO         bool
	CloudImage  bool
	Netboot     bool
	RawDisk     bool
	Tar         bool
	GCE         bool
	VHD         bool
	MAAS        bool
	FIPS        bool
	TrustedBoot bool
}

// UKIISO reports whether the ISO this build produces is the signed UKI
// installer rather than the GRUB one.
//
// Trusted Boot is the only mode that produces one: kairos-init -t builds an
// image with no GRUB and no separate initrd, so the only ISO it can be
// installed from is the UKI ISO. API and UI therefore offer a single ISO
// option and the boot mode decides which one gets built, rather than a
// separate uki flag that is only ever valid together with trustedBoot
// (kairos-io/kairos#5497).
func (o OutputOptions) UKIISO() bool { return o.TrustedBoot && o.ISO }

// SigningOptions holds SecureBoot / UKI signing paths.
type SigningOptions struct {
	UKISecureBootKey    string
	UKISecureBootCert   string
	UKITPMPCRKey        string
	UKIPublicKeysDir    string
	UKISecureBootEnroll string
}

// ProvisioningOptions controls post-build provisioning behaviour.
type ProvisioningOptions struct {
	AutoInstall        bool
	RegisterAuroraBoot bool
	TargetGroupID      string
	// KubernetesEnabled controls whether k3s/k0s starts on first boot on a
	// Standard-variant image. The handler resolves the request's tri-state
	// (nil defaults to true) into this bool before handing it to the builder,
	// which persists it on the artifact record so the frontend can rehydrate
	// the Kubernetes card when cloning.
	KubernetesEnabled bool
	// AllowedCommands is the explicit phonehome.allowed_commands list baked
	// into the cloud-config. The AuroraBoot backend substitutes the safe
	// default set when the caller leaves this nil, so the emitted YAML always
	// carries the key — nodes never inherit an implicit agent-side default.
	AllowedCommands []string
}

// BuildOptions describes what to build.
type BuildOptions struct {
	ID   string // unique build ID
	Name string // optional friendly name

	// UploadToken is the per-build bearer the operator backend's exporter
	// Job uses to PUT finished artifacts back to AuroraBoot's upload
	// endpoint. Populated by the Create handler on every build regardless
	// of backend; the local backend simply ignores it. Never derived from
	// the user request.
	UploadToken string

	// LogRedactValues are substrings the builder scrubs from every log line
	// before persisting to the store or broadcasting to the UI. The Create
	// handler populates this with the secrets it hands the builder: the
	// registration token and node password it injects into the
	// cloud-config, and the UploadToken. A build step that prints one of
	// them therefore cannot land it in the GET /logs response or the live
	// log pane. Best-effort: values shorter than 8 characters are skipped
	// to avoid replacing unrelated text that happens to contain them, and
	// only verbatim occurrences are redacted - anything transformed
	// (base64, JSON-escaped) still passes.
	LogRedactValues []string

	// Grouped options (preferred for new code).
	Source       ImageSource
	Outputs      OutputOptions
	Signing      SigningOptions
	Provisioning ProvisioningOptions

	// Legacy flat fields — kept for backward compatibility with existing callers.
	// New code should use the grouped sub-structs above instead.
	BaseImage         string // e.g., "quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.6.0"
	KairosVersion     string
	Model             string
	KubernetesDistro  string
	KubernetesVersion string
	FIPS              bool
	TrustedBoot       bool
	ISO               bool
	CloudImage        bool
	Netboot           bool

	CloudConfig string // YAML cloud-config to bake in
	OutputDir   string // where to write artifacts

	// Customization options:
	OverlayRootfs   string // path to overlay dir (files copied on top of rootfs)
	OverlayID       string // ID of the uploaded overlay that OverlayRootfs points at; stored on the artifact record
	Dockerfile      string // optional Dockerfile content (builds image via docker before ISO)
	BuildContextDir string // directory with files available to COPY in Dockerfile
	KairosInitImage string

	// Hadron composition (metadata only — not consumed by the build; the
	// rendered Dockerfile is what actually runs). Persisted on the artifact
	// record so the frontend can rehydrate the composer when cloning.
	HadronBase     string
	HadronFirmware []string
	HadronLayers   []string
	HadronExtra    string

	// Extensions are the catalog extensions to materialize in the ISO root,
	// as name or name@version. The handler validates every entry before the
	// build starts; the builder resolves them against ExtensionsCatalogs.
	Extensions []string
	// ExtensionsCatalogs are the catalog URLs or paths to search, in order.
	// Empty means the build reads extensions.DefaultCatalog.
	ExtensionsCatalogs []string

	// ExtensionHierarchies carries the operator-supplied sysext/confext
	// mount points. The handler validates and normalizes both lists; /usr
	// (sysext) and /etc (confext) are implicit and never appear here. The
	// local builder persists the pair on the artifact record so a full-row
	// Save on cancel/complete cannot blank it out, and the handler bakes
	// the matching SYSTEMD_{SYSEXT,CONFEXT}_HIERARCHIES drop-in into the
	// cloud-config so the built image boots with the requested scope set.
	ExtensionHierarchies ExtensionHierarchies
}

// ExtensionHierarchies mirrors store.ExtensionHierarchies without importing
// the store package. Slices are the normalized paths (validated by the
// handler, /usr and /etc stripped).
type ExtensionHierarchies struct {
	Sysext  []string
	Confext []string
}

// BuildStatus tracks the state of a build.
type BuildStatus struct {
	ID        string   `json:"id"`
	Phase     string   `json:"phase"` // Pending, Building, Ready, Error
	Message   string   `json:"message"`
	Artifacts []string `json:"artifacts"` // paths of the built files on the server; API responses carry base names
}

// Build phases.
const (
	BuildPending  = "Pending"
	BuildBuilding = "Building"
	BuildReady    = "Ready"
	BuildError    = "Error"
)

// ArtifactBuilder builds Kairos artifacts.
type ArtifactBuilder interface {
	Build(ctx context.Context, opts BuildOptions) (*BuildStatus, error)
	Status(ctx context.Context, id string) (*BuildStatus, error)
	List(ctx context.Context) ([]*BuildStatus, error)
	Cancel(ctx context.Context, id string) error
}
