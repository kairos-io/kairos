package builder

import (
	"context"
	"fmt"
)

// ValidatingArtifactBuilder checks the image references and the output
// formats in a build request before handing it to the wrapped builder, so no
// backend has to repeat the checks and none can forget them. Status, List and Cancel pass straight through.
type ValidatingArtifactBuilder struct {
	ArtifactBuilder
}

// NewValidatingArtifactBuilder wraps next with build request validation.
func NewValidatingArtifactBuilder(next ArtifactBuilder) *ValidatingArtifactBuilder {
	return &ValidatingArtifactBuilder{ArtifactBuilder: next}
}

// Build rejects opts with ErrInvalidBuildOptions when an image reference is
// not a plain OCI reference or an output does not go with the boot mode, and
// otherwise calls the wrapped builder.
func (v *ValidatingArtifactBuilder) Build(ctx context.Context, opts BuildOptions) (*BuildStatus, error) {
	if err := opts.ValidateImageRefs(); err != nil {
		return nil, err
	}
	if err := opts.ValidateOutputs(); err != nil {
		return nil, err
	}
	return v.ArtifactBuilder.Build(ctx, opts)
}

// ValidatingExtensionBuilder is the ExtensionBuilder counterpart of
// ValidatingArtifactBuilder.
type ValidatingExtensionBuilder struct {
	ExtensionBuilder
}

// NewValidatingExtensionBuilder wraps next with image reference validation.
func NewValidatingExtensionBuilder(next ExtensionBuilder) *ValidatingExtensionBuilder {
	return &ValidatingExtensionBuilder{ExtensionBuilder: next}
}

// Build rejects opts with ErrInvalidBuildOptions when the source image is not
// a plain OCI reference, and otherwise calls the wrapped builder.
func (v *ValidatingExtensionBuilder) Build(ctx context.Context, opts ExtensionBuildOptions) (*ExtensionBuildStatus, error) {
	if err := opts.ValidateImageRefs(); err != nil {
		return nil, err
	}
	return v.ExtensionBuilder.Build(ctx, opts)
}

// ValidateImageRefs checks the source image an extension build writes into a
// generated Dockerfile or hands to the sysext tooling. The error wraps
// ErrInvalidBuildOptions.
func (o ExtensionBuildOptions) ValidateImageRefs() error {
	if o.Source.BaseImage == "" {
		return nil
	}
	if err := ValidateImageRef("source.baseImage", o.Source.BaseImage); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBuildOptions, err)
	}
	return nil
}
