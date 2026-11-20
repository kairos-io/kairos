package builder

import "context"

// ExtensionSource describes where the sysext/confext content comes from.
// Exactly one of {BaseImage, SourceArtifactID, Dockerfile} is meaningful,
// chosen by Mode. ExtraSteps is only meaningful with Mode = "artifact".
type ExtensionSource struct {
	Mode             string // "artifact" | "image" | "dockerfile"
	SourceArtifactID string // when Mode = "artifact"
	BaseImage        string // when Mode = "image", or resolved at build time for Mode = "artifact"
	Dockerfile       string // when Mode = "dockerfile" or "artifact"+ExtraSteps
	ExtraSteps       string // optional, only with Mode = "artifact"
	BuildContextDir  string // mirrors ArtifactBuilder.BuildContextDir for COPY support
}

// ExtensionSigning carries the PEM files to sign the .raw with. Empty
// strings mean unsigned. The handler resolves these from a SecureBootKeySet
// before calling Build.
type ExtensionSigning struct {
	PrivateKey  string // file path
	Certificate string // file path
}

// ExtensionBuildOptions describes what to build.
type ExtensionBuildOptions struct {
	ID            string
	Name          string
	Type          string // "sysext" | "confext"
	Arch          string // "amd64" | "arm64" | "riscv64"
	Version       string
	Source        ExtensionSource
	Signing       ExtensionSigning
	Hierarchies   []string // sysext-only; /usr implicit
	ServiceReload bool     // sysext-only
	// SigningKeySetID is the secure-boot key set the caller selected, kept
	// alongside the resolved Signing file paths so the builder can write it
	// into the initial record. The handler must not attach it afterwards: the
	// store's Create is a full-row upsert and the build goroutine starts
	// before Build returns, so a second read-modify-write races the
	// goroutine's first phase update and can blank the column for good.
	SigningKeySetID string
	// DownloadToken is the per-extension download bearer the handler mints.
	// Threaded through for the same reason as SigningKeySetID: it has to be in
	// the row the synchronous Create persists, because attaching it afterwards
	// races the build goroutine's first full-row phase upsert.
	DownloadToken string
}

// ExtensionBuildStatus tracks an extension build's state. Phase strings
// match the existing BuildPending / BuildBuilding / BuildReady / BuildError
// constants from this package.
type ExtensionBuildStatus struct {
	ID             string `json:"id"`
	Phase          string `json:"phase"`
	Message        string `json:"message"`
	RawFile        string `json:"rawFile"`
	ContainerImage string `json:"containerImage"`
}

// ExtensionBuilder builds Kairos extensions (.raw files).
//
// The interface is the swap point for a future Kubernetes-operator-backed
// implementation: the production in-process builder lives in
// internal/builder/auroraboot/extension.go, but a k8s controller-backed
// implementation can satisfy this interface by translating Build/Cancel
// calls into Custom Resource operations and Status/List into watches.
type ExtensionBuilder interface {
	Build(ctx context.Context, opts ExtensionBuildOptions) (*ExtensionBuildStatus, error)
	Status(ctx context.Context, id string) (*ExtensionBuildStatus, error)
	List(ctx context.Context) ([]*ExtensionBuildStatus, error)
	Cancel(ctx context.Context, id string) error
}
