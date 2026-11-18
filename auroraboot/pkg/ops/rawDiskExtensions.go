package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
)

// BundledExtensionsDir is where a disk build stages the extension images it
// baked in, relative to the root of the OEM partition.
const BundledExtensionsDir = "extensions"

// BundledExtensionsCloudConfig is the cloud config that installs the staged
// images onto the persistent partition.
//
// It sorts after 01_reset.yaml so the reset that file triggers has already
// created and formatted the persistent partition by the time this one runs.
// Unlike 01_reset.yaml it is not removed afterwards: a reset formats the
// persistent partition, and this is what puts the extensions back.
const BundledExtensionsCloudConfig = "02_extensions.yaml"

// DiskExtensions is the extension payload a raw-disk build bakes into the
// image. The names come from the artifact spec and are resolved against the
// catalogs at build time, so the installed system needs no network to get
// them.
type DiskExtensions struct {
	// Requests are the catalog extensions the artifact spec named.
	Requests []extensions.Request
	// Catalogs are searched in order. Empty means extensions.DefaultCatalog.
	Catalogs []string
	// Architecture the resolved extension images must be built for.
	Architecture string
	// Insecure allows pulling the extension images from an insecure registry.
	Insecure bool
}

// withArchFrom fills in the architecture from the rootfs being packed when the
// build did not state one, the same fallback GenISO applies. An extension is
// resolved per architecture, so guessing the host's would hand a cross-arch
// build images its target cannot run.
func (d DiskExtensions) withArchFrom(rootfs string) DiskExtensions {
	if d.Architecture != "" || len(d.Requests) == 0 {
		return d
	}
	arch, err := utils.GetArchFromRootfs(rootfs, internal.Log)
	if err != nil {
		internal.Log.Logger.Error().Err(err).Msg("failed to detect arch from rootfs; falling back to host arch")
	}
	if arch == "" {
		arch = runtime.GOARCH
	}
	d.Architecture = arch
	return d
}

// materializeDiskExtensions downloads the requested extensions into a
// temporary directory and returns their paths together with the cleanup that
// removes them. The images are copied into the OEM staging directory later, so
// nothing needs them once the build has assembled that partition.
func materializeDiskExtensions(ctx context.Context, spec DiskExtensions) ([]string, func(), error) {
	noop := func() {}
	if len(spec.Requests) == 0 {
		return nil, noop, nil
	}

	staging, err := os.MkdirTemp("", "auroraboot-disk-extensions")
	if err != nil {
		return nil, noop, fmt.Errorf("creating the extension staging directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(staging) }

	images, err := materializeExtensionArtifacts(ctx, spec.Catalogs, spec.Requests, spec.Architecture, staging, spec.Insecure)
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	return images, cleanup, nil
}

// stageBundledExtensions copies the materialized extension images into the OEM
// staging directory and writes the cloud config that installs them. It returns
// the number of bytes staged so the caller can size the OEM partition to hold
// them.
func (r *RawImage) stageBundledExtensions(oemDir string) (int64, error) {
	if len(r.ExtensionFiles) == 0 {
		return 0, nil
	}

	destination := filepath.Join(oemDir, BundledExtensionsDir)
	if err := fsutils.MkdirAll(r.config.Fs, destination, 0755); err != nil {
		return 0, fmt.Errorf("creating %s: %w", destination, err)
	}

	var staged int64
	for _, image := range r.ExtensionFiles {
		name := filepath.Base(image)
		if filepath.Ext(name) != ".raw" {
			// immucore merges only .raw entries, so staging anything else
			// would cost the disk space and still not reach /run/extensions.
			return 0, fmt.Errorf("extension %q is not a .raw image", name)
		}
		info, err := r.config.Fs.Stat(image)
		if err != nil {
			return 0, fmt.Errorf("reading extension %s: %w", image, err)
		}
		if err := fsutils.Copy(r.config.Fs, image, filepath.Join(destination, name)); err != nil {
			return 0, fmt.Errorf("staging extension %s: %w", name, err)
		}
		staged += info.Size()
		internal.Log.Logger.Debug().Str("extension", name).Int64("bytes", info.Size()).Msg("Staged extension into the OEM partition")
	}

	target := filepath.Join(oemDir, BundledExtensionsCloudConfig)
	if err := r.config.Fs.WriteFile(target, []byte(BundledExtensionsStage()), 0o644); err != nil {
		return 0, fmt.Errorf("writing %s: %w", target, err)
	}

	return staged, nil
}

// oemPartitionSize returns the size in MiB of an OEM partition that has to hold
// extensionBytes of staged extension images on top of its usual contents.
//
// The default size is kept as headroom rather than consumed, because the
// cloud configs already there and the filesystem's own metadata grow with the
// partition, and a full OEM partition fails the build at deploy time.
func oemPartitionSize(extensionBytes int64) uint {
	if extensionBytes <= 0 {
		return sdkConstants.OEMSize
	}
	const mib = 1024 * 1024
	return sdkConstants.OEMSize + uint((extensionBytes+mib-1)/mib)
}

// BundledExtensionsStage is the cloud config that installs the staged
// extension images onto the persistent partition, in the layout
// hooks.ExtensionsPostInstall would have produced on an ISO install: the
// images under the bind source immucore syncs into /var/lib/kairos/extensions,
// and a relative link per boot state, because immucore populates
// /run/extensions from <dir>/<boot state> and merges nothing that is only in
// <dir>.
//
// It runs in the after-reset stage rather than after-reset-chroot: resetting
// with FormatPersistent leaves the persistent partition unmounted, so the
// chroot's /usr/local would be an empty directory on the recovery rootfs and
// every write would be discarded on the reboot. Mounting the partition by
// label here makes the destination the partition whether or not the reset
// left it mounted.
//
// Recovery is deliberately not linked, matching the boot states the install
// hook enables.
func BundledExtensionsStage() string {
	return fmt.Sprintf(`name: Install the extensions bundled into the disk image
stages:
    after-reset:
        - name: Install bundled system extensions
          if: '[ -d /oem/%[1]s ]'
          commands:
            - |
              set -e
              mountpoint="$(mktemp -d)"
              mount -L %[2]s "$mountpoint"
              extensions="$mountpoint/.state/var-lib-kairos.bind/extensions"
              mkdir -p "$extensions/active" "$extensions/passive"
              for image in /oem/%[1]s/*.raw; do
                  [ -e "$image" ] || continue
                  name="$(basename "$image")"
                  cp -f "$image" "$extensions/$name"
                  ln -sfn "../$name" "$extensions/active/$name"
                  ln -sfn "../$name" "$extensions/passive/$name"
              done
              sync
              umount "$mountpoint"
              rmdir "$mountpoint"
`, BundledExtensionsDir, sdkConstants.PersistentLabel)
}
