package operator

import (
	"fmt"

	buildv1alpha2 "github.com/kairos-io/kairos-operator/api/v1alpha2"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
)

// translateBuildOptions maps AuroraBoot BuildOptions to an OSArtifactSpec.
//
// Notes on quirks:
//   - Outputs.CloudImage and Outputs.RawDisk both map to Artifacts.CloudImage
//     because the operator has a single "cloud image" output; the local builder
//     historically distinguished the two.
//   - Outputs.Tar has no explicit toggle on the CRD; the operator always
//     writes /artifacts/<name>.tar as a byproduct of the buildah stage. Setting
//     Tar alone is therefore not an error even though no field carries it.
func translateBuildOptions(id string, opts builder.BuildOptions) (buildv1alpha2.OSArtifactSpec, error) {
	arch := opts.Source.Arch
	switch arch {
	case "", "amd64", "arm64":
	default:
		return buildv1alpha2.OSArtifactSpec{}, fmt.Errorf("%w: source.arch must be 'amd64' or 'arm64', got %q", builder.ErrInvalidBuildOptions, arch)
	}

	// kairos-operator's ArtifactSpec has no MAAS output and no way to add
	// catalog extensions, so a request for either would finish without
	// them. Reject it instead. A catalog list with no extensions resolves
	// nothing on either backend, so it is left alone.
	if opts.Outputs.MAAS {
		return buildv1alpha2.OSArtifactSpec{}, fmt.Errorf("%w: outputs.maas is not supported by the operator backend", builder.ErrInvalidBuildOptions)
	}
	if len(opts.Extensions) > 0 {
		return buildv1alpha2.OSArtifactSpec{}, fmt.Errorf("%w: extensions are not supported by the operator backend", builder.ErrInvalidBuildOptions)
	}

	// A caller may pass the base image via the legacy flat field
	// (opts.BaseImage) or the grouped shape (opts.Source.BaseImage). Both are
	// valid; treat them as one so validation and translation cannot disagree.
	baseRef := firstNonEmpty(opts.BaseImage, opts.Source.BaseImage)
	kairosVersion := firstNonEmpty(opts.Source.KairosVersion, opts.KairosVersion)

	// Pre-built means: consume the caller's ref as-is, no kairos-init pass.
	// Any signal that we should do build-time work (a Dockerfile, an
	// explicit KairosVersion, or a KairosInitImage override) drops us out
	// of the pre-built branch so the caller's intent lands on the
	// operator's kairos-init flow rather than getting silently ignored.
	//
	// KubernetesDistro/KubernetesVersion are NOT signals here. kairos-init
	// installs a provider only when it builds Kairos from a plain OS base
	// (Variant=Standard, controlled by -p); it does not layer a provider
	// onto an existing Kairos image. The local backend's ensureKairosified
	// short-circuits on /etc/kairos-release and never runs kairos-init in
	// that case either, so "prebuilt Kairos + KubernetesDistro=k3s"
	// silently drops the k8s knob on both backends. Rejecting the
	// combination outright is the cleaner fix but belongs to a shared
	// change across both backends, not to this translator alone.
	preBuilt := baseRef != "" &&
		opts.Dockerfile == "" &&
		kairosVersion == "" &&
		opts.KairosInitImage == ""

	// FIPS and TrustedBoot are build-time flags passed to kairos-init; they
	// only make sense when we actually build the Stage-1 image. The legacy
	// flat and grouped fields are OR'd so a caller setting either shape gets
	// the same validation and the flag can never be silently dropped by the
	// pre-built branch, which carries no kairos-init BuildOptions.
	fips := opts.Outputs.FIPS || opts.FIPS
	trustedBoot := opts.Outputs.TrustedBoot || opts.TrustedBoot
	if preBuilt && fips {
		return buildv1alpha2.OSArtifactSpec{}, fmt.Errorf("%w: outputs.fips requires a from-scratch build, not a pre-built base image", builder.ErrInvalidBuildOptions)
	}
	if preBuilt && trustedBoot {
		return buildv1alpha2.OSArtifactSpec{}, fmt.Errorf("%w: outputs.trustedBoot requires a from-scratch build, not a pre-built base image", builder.ErrInvalidBuildOptions)
	}

	spec := buildv1alpha2.OSArtifactSpec{}

	switch {
	case preBuilt:
		spec.Image = buildv1alpha2.ImageSpec{Ref: baseRef}
	case isHadronBuild(opts):
		// The UI's Hadron composer produces a Dockerfile that is base +
		// firmware compatibility guard + firmware/layer COPYs. It does NOT
		// include the kairos-init RUN block, so the resulting image has no
		// kernel/initrd and the downstream ISO stage fails with "no initrd
		// file found". Send OCISpec + BuildOptions so the operator wraps the
		// composed content: FROM ${HadronBase} at the top (injected by the
		// operator from BuildOptions.BaseImage), then our middle content
		// (compatibility guard + COPYs), then the kairos-init COPY+RUN at the bottom.
		version := kairosVersion
		if version == "" {
			version = "latest"
		}
		spec.Image = buildv1alpha2.ImageSpec{
			BuildOptions: &buildv1alpha2.BuildOptions{
				Version:           version,
				BaseImage:         opts.HadronBase,
				Model:             firstNonEmpty(opts.Source.Model, opts.Model),
				TrustedBoot:       trustedBoot,
				KubernetesDistro:  firstNonEmpty(opts.Source.KubernetesDistro, opts.KubernetesDistro),
				KubernetesVersion: firstNonEmpty(opts.Source.KubernetesVersion, opts.KubernetesVersion),
				FIPS:              fips,
				KairosInitImage:   opts.KairosInitImage,
			},
			OCISpec: &buildv1alpha2.OCISpec{
				Ref: &buildv1alpha2.SecretKeySelector{
					Name: dockerfileSecretName(id),
					Key:  dockerfileSecretKey,
				},
			},
		}
	case opts.Dockerfile != "":
		spec.Image = buildv1alpha2.ImageSpec{
			OCISpec: &buildv1alpha2.OCISpec{
				Ref: &buildv1alpha2.SecretKeySelector{
					Name: dockerfileSecretName(id),
					Key:  dockerfileSecretKey,
				},
			},
		}
	default:
		// The local backend defaults an empty KairosVersion to "latest" inside
		// ensureKairosified before it invokes kairos-init. Mirror that so
		// callers who signal from-scratch intent (KairosInitImage set,
		// FIPS/TrustedBoot flag, ...) without naming a version get the same
		// behaviour on the operator backend instead of a validation error
		// from the operator's CRD (Version required).
		version := kairosVersion
		if version == "" {
			version = "latest"
		}
		spec.Image = buildv1alpha2.ImageSpec{
			BuildOptions: &buildv1alpha2.BuildOptions{
				Version:           version,
				BaseImage:         baseRef,
				Model:             firstNonEmpty(opts.Source.Model, opts.Model),
				TrustedBoot:       trustedBoot,
				KubernetesDistro:  firstNonEmpty(opts.Source.KubernetesDistro, opts.KubernetesDistro),
				KubernetesVersion: firstNonEmpty(opts.Source.KubernetesVersion, opts.KubernetesVersion),
				FIPS:              fips,
				KairosInitImage:   opts.KairosInitImage,
			},
		}
	}

	artifacts := &buildv1alpha2.ArtifactSpec{Arch: arch}
	// A Trusted Boot build asks for the ISO and means the UKI one, which the
	// UKI spec below requests. Asking for both would make the operator build
	// a GRUB ISO the Trusted Boot image cannot be installed from.
	if (opts.Outputs.ISO || opts.ISO) && !opts.Outputs.UKIISO() {
		artifacts.ISO = true
	}
	if opts.Outputs.CloudImage || opts.Outputs.RawDisk || opts.CloudImage {
		artifacts.CloudImage = true
	}
	if opts.Outputs.Netboot || opts.Netboot {
		artifacts.Netboot = true
	}
	if opts.Outputs.GCE {
		artifacts.GCEImage = true
	}
	if opts.Outputs.VHD {
		artifacts.AzureImage = true
	}
	if opts.Outputs.UKIISO() {
		artifacts.UKI = &buildv1alpha2.UKISpec{ISO: true}
	}
	if opts.CloudConfig != "" {
		artifacts.CloudConfigRef = &buildv1alpha2.SecretKeySelector{
			Name: cloudConfigSecretName(id),
			Key:  cloudConfigSecretKey,
		}
	}
	spec.Artifacts = artifacts

	return spec, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// isHadronBuild reports whether the caller's options describe a Hadron build.
// The UI's Hadron composer sets HadronBase alongside the composed middle
// content in Dockerfile; no other flow sets HadronBase, so its presence
// uniquely identifies a build whose middle content lacks a base FROM and
// kairos-init - it needs the operator to inject both via BuildOptions.
// Plain custom-Dockerfile builds (HadronBase empty) stay in the advanced
// branch that submits the Dockerfile verbatim.
func isHadronBuild(opts builder.BuildOptions) bool {
	return opts.HadronBase != "" && opts.Dockerfile != ""
}
