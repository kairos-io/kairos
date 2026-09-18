package hook

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	installer "github.com/kairos-io/kairos/v4/agent/pkg/extensions"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	extensiontypes "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
	"github.com/mudler/yip/pkg/schema"
)

// PersistentExtensionsDir is where the extensions of the installed system are
// staged from the live media.
//
// On a running node extensions live in /var/lib/kairos/extensions, which
// immucore bind mounts from the persistent partition. The bind source is
// derived from the path by replacing the separators and appending `.bind`, so
// while installing, with the persistent partition mounted at /usr/local, the
// same directory is reachable here. immucore's first-boot sync into the bind
// directory is an `rsync -aquAX` with no --delete, so what we stage survives it.
const PersistentExtensionsDir = constants.UsrLocalPath + "/.state/var-lib-kairos.bind/extensions"

// permissiveImagePolicy is a systemd image policy that accepts an extension
// however it is protected, including not at all. It spells out every flag
// rather than using systemd's `open` alias so a typo fails at review instead
// of at boot: it is the same set (see systemd.image-policy(7)).
const permissiveImagePolicy = "root=verity+signed+encrypted+unprotected+absent:usr=verity+signed+encrypted+unprotected+absent"

// ExtensionsPostInstall installs the extensions declared under
// install.extensions, plus the ones shipped on the live media, onto the system
// being installed.
//
// It only covers the non-UKI layout. Under UKI, extensions live in the EFI
// partition rather than in the persistent one, and SysExtPostInstall already
// owns that mount window, so it installs them there.
type ExtensionsPostInstall struct{}

func (ExtensionsPostInstall) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if internalutils.IsUki() {
		return nil
	}

	var declared extensiontypes.Extensions
	if c.Install != nil {
		declared = c.Install.Extensions
	}
	// Read the media before mounting anything, so that an install with
	// nothing to do still touches no partition.
	onMedia, err := LiveMediaExtensions(c)
	if err != nil {
		return fmt.Errorf("looking for extensions on the live media: %w", err)
	}
	if len(declared) == 0 && len(onMedia) == 0 {
		return nil
	}
	c.Logger.Logger.Info().Int("declared", len(declared)).Int("on_media", len(onMedia)).Msg("Running ExtensionsPostInstall hook")

	// BundlePostInstall may have left these mounted; start from a known state.
	_ = machine.Umount(constants.PersistentDir) //nolint:errcheck

	if err := machine.Mount(constants.PersistentLabel, constants.UsrLocalPath); err != nil {
		return fmt.Errorf("mounting the persistent partition to install extensions: %w", err)
	}
	defer func() {
		if err := machine.Umount(constants.UsrLocalPath); err != nil {
			c.Logger.Errorf("could not unmount persistent partition: %s", err)
		}
	}()

	if err := fsutils.MkdirAll(c.Fs, PersistentExtensionsDir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", PersistentExtensionsDir, err)
	}
	installed, err := installer.InstallDeclared(&c, declared, PersistentExtensionsDir)
	if err != nil {
		return err
	}
	staged, err := StageLiveMediaExtensions(c, installed, PersistentExtensionsDir)
	if err != nil {
		return fmt.Errorf("staging the extensions on the live media: %w", err)
	}
	if err := EnableExtensionsForBoot(c, PersistentExtensionsDir, append(installed, staged...)); err != nil {
		return err
	}

	c.Logger.Logger.Info().Msg("Finish ExtensionsPostInstall hook")
	return nil
}

// BootStatesEnabledOnInstall are the boot states an extension declared under
// install.extensions is enabled for. It is active and passive, and not
// recovery, so that it matches the UKI layout, where SysExtPostInstall writes
// into active.efi.extra.d and passive.efi.extra.d and nowhere else.
var BootStatesEnabledOnInstall = []string{constants.BootActive, constants.BootPassive}

// EnableExtensionsForBoot enables the named extensions of dir by linking each
// one into the per boot state sub-directory that immucore reads.
//
// Staging the image is not enough on its own. immucore looks only at
// <dir>/<boot state> when it populates /run/extensions, so an extension that
// is only in dir is never merged, systemd-sysext.service does not even start
// (all four of its ConditionDirectoryNotEmpty= fail) and the node boots
// without it. This is the step `kairos-agent sysext enable` performs on a
// running node, done here for what the install declared.
//
// The links are relative because the directory is written through the
// persistent partition mounted at /usr/local and read back at
// /var/lib/kairos/extensions. A relative link resolves under both.
func EnableExtensionsForBoot(c sdkConfig.Config, dir string, names []string) error {
	var images []string
	for _, name := range names {
		if filepath.Ext(name) != ".raw" {
			// immucore links only .raw entries, so anything else would be
			// a link nothing ever follows.
			c.Logger.Logger.Warn().Str("extension", name).Msg("Not enabling an extension that is not a .raw image")
			continue
		}
		images = append(images, name)
	}
	if len(images) == 0 {
		return nil
	}

	for _, bootState := range BootStatesEnabledOnInstall {
		stateDir := filepath.Join(dir, bootState)
		if err := fsutils.MkdirAll(c.Fs, stateDir, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", stateDir, err)
		}
		for _, name := range images {
			link := filepath.Join(stateDir, name)
			// A re-run of the installer must not trip over the link it
			// left behind, and the image it points at may have changed.
			if err := c.Fs.Remove(link); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("replacing %s: %w", link, err)
			}
			if err := c.Fs.Symlink(filepath.Join("..", name), link); err != nil {
				return fmt.Errorf("enabling %s for %s: %w", name, bootState, err)
			}
			c.Logger.Logger.Info().Str("extension", name).Str("boot_state", bootState).Msg("Enabled extension")
		}
	}
	return nil
}

// installDeclaredExtensionsToEFI stages the declared extensions into the UKI
// extension directories. It is called by SysExtPostInstall, with the EFI
// partition already mounted and both directories already created.
//
// The download goes to a temporary directory first: the same image has to end
// up in both the active and the passive directory, and pulling it twice would
// double the transfer for no gain.
func installDeclaredExtensionsToEFI(c sdkConfig.Config, targets ...string) ([]string, error) {
	if c.Install == nil || len(c.Install.Extensions) == 0 {
		return nil, nil
	}
	c.Logger.Logger.Info().Int("extensions", len(c.Install.Extensions)).Msg("Installing declared extensions into the EFI partition")

	staging, err := fsutils.TempDir(c.Fs, "", "kairos-extensions-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Fs.RemoveAll(staging) }()

	installed, err := installer.InstallDeclared(&c, c.Install.Extensions, staging)
	if err != nil {
		return nil, err
	}

	for _, name := range installed {
		for _, target := range targets {
			if err := fsutils.Copy(c.Fs, filepath.Join(staging, name), filepath.Join(target, name)); err != nil {
				return installed, fmt.Errorf("copying extension %s to %s: %w", name, target, err)
			}
			c.Logger.Debugf("copied %s to %s", name, target)
		}
	}
	return installed, nil
}

// ExtensionSignaturePolicy relaxes the systemd image policy when
// `extensions.ignore_signatures` is set.
//
// systemd refuses an extension that does not satisfy the policy on the
// systemd-sysext and systemd-confext units, which kairos-init sets to require
// a signature under Trusted Boot and dm-verity otherwise. Extensions published
// to a catalog are not signed, so on such a node they are downloaded, enabled
// and then silently not merged. This is the opt-out, written as a cloud config
// to /oem so that it also survives an upgrade, the same way SSHHardening does.
//
// The drop-in is named to sort after kairos-init's own (`kairos.conf`,
// `kairos-uki.conf`), because the last drop-in to set ExecStart is the one
// that counts.
type ExtensionSignaturePolicy struct{}

func (ExtensionSignaturePolicy) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if !c.Extensions.IgnoreSignatures {
		return nil
	}
	c.Logger.Logger.Warn().Msg("extensions.ignore_signatures is set: extensions will be merged without a signature systemd can verify")

	if err := machine.Mount("COS_OEM", constants.OEMPath); err != nil {
		return err
	}
	defer func() {
		_ = machine.Umount(constants.OEMPath)
	}()

	return saveCloudConfig("extensions_ignore_signatures", IgnoreSignaturesCloudConfig())
}

// IgnoreSignaturesCloudConfig is the cloud config ExtensionSignaturePolicy
// writes. It is exported so a test can read what gets installed without
// mounting anything.
func IgnoreSignaturesCloudConfig() schema.YipConfig {
	var files []schema.File
	for _, command := range []string{"systemd-sysext", "systemd-confext"} {
		files = append(files, schema.File{
			Path:        fmt.Sprintf("/etc/systemd/system/%s.service.d/zz-kairos-ignore-signatures.conf", command),
			Permissions: 0o644,
			Owner:       0,
			Group:       0,
			Content: fmt.Sprintf(`# Managed by kairos-agent (extensions.ignore_signatures: true).
# Accepts an extension however it is protected, including not at all.
[Service]
ExecStart=
ExecStart=%[1]s refresh --image-policy="%[2]s"
ExecReload=
ExecReload=%[1]s refresh --image-policy="%[2]s"
`, command, permissiveImagePolicy),
		})
	}

	return schema.YipConfig{
		Stages: map[string][]schema.Stage{
			"boot": {
				{
					Name:  "Ignore extension signatures",
					Files: files,
					Commands: []string{
						// The units have already started with the strict
						// policy by the time the boot stage runs, so pick the
						// drop-in up and merge again rather than waiting for
						// the next boot.
						"systemctl daemon-reload",
						"systemctl restart systemd-sysext systemd-confext || true",
					},
				},
			},
		},
	}
}

// LiveExtensionSuffix is the file name suffix that marks an extension image
// shipped on the live media. It is the same suffix the sysext tooling and the
// catalog use, so an image published to a catalog can be dropped on an ISO
// unchanged.
const LiveExtensionSuffix = ".sysext.raw"

// LiveMediaExtensions lists the extension images shipped on the live media.
//
// AuroraBoot lands an `iso.overlay_iso` tree at the ISO root, and the ISO root
// is mounted at constants.LiveDir while the installer runs, on both the GRUB
// and the UKI flow. So dropping a `<name>.sysext.raw` next to the ISO's
// config.yaml is how an artifact ships an extension with no network at install
// time.
//
// A missing directory is not an error: an install that did not boot from
// removable media has no live directory at all.
func LiveMediaExtensions(c sdkConfig.Config) ([]string, error) {
	if _, err := c.Fs.Stat(constants.LiveDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var found []string
	err := fsutils.WalkDirFs(c.Fs, constants.LiveDir, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), LiveExtensionSuffix) {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// StageLiveMediaExtensions copies every extension image found on the live
// media into each of targets, and returns the file names it copied.
//
// A name already in skip is left alone: the config declared it, so it has been
// installed already and re-copying it would only risk replacing a resolved
// image with whatever happens to sit on the media under the same name.
//
// Each target directory has to exist. A copy that fails is reported and the
// sweep moves on, unless FailOnBundleErrors is set, which is the same contract
// the rest of the post-install hooks follow.
func StageLiveMediaExtensions(c sdkConfig.Config, skip []string, targets ...string) ([]string, error) {
	paths, err := LiveMediaExtensions(c)
	if err != nil {
		return nil, err
	}

	skipped := map[string]bool{}
	for _, name := range skip {
		skipped[name] = true
	}

	var staged []string
	for _, path := range paths {
		name := filepath.Base(path)
		if skipped[name] {
			c.Logger.Logger.Debug().Str("extension", name).Msg("Not staging a live media extension the config already declared")
			continue
		}

		copied := true
		for _, target := range targets {
			dst := filepath.Join(target, name)
			if err := fsutils.Copy(c.Fs, path, dst); err != nil {
				c.Logger.Errorf("failed to copy %s to %s: %s", path, target, err)
				if c.FailOnBundleErrors {
					return staged, err
				}
				copied = false
				break
			}
			c.Logger.Debugf("copied %s to %s", path, target)
		}
		if copied {
			staged = append(staged, name)
		}
	}
	return staged, nil
}
