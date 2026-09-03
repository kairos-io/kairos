package hook

import (
	"fmt"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	installer "github.com/kairos-io/kairos/v4/agent/pkg/extensions"
	internalutils "github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
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
// install.extensions onto the system being installed.
//
// It only covers the non-UKI layout. Under UKI, extensions live in the EFI
// partition rather than in the persistent one, and SysExtPostInstall already
// owns that mount window, so it installs them there.
type ExtensionsPostInstall struct{}

func (ExtensionsPostInstall) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if c.Install == nil || len(c.Install.Extensions) == 0 {
		return nil
	}
	if internalutils.IsUki() {
		return nil
	}
	c.Logger.Logger.Info().Int("extensions", len(c.Install.Extensions)).Msg("Running ExtensionsPostInstall hook")

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
	if err := installer.InstallDeclared(&c, c.Install.Extensions, PersistentExtensionsDir); err != nil {
		return err
	}

	c.Logger.Logger.Info().Msg("Finish ExtensionsPostInstall hook")
	return nil
}

// installDeclaredExtensionsToEFI stages the declared extensions into the UKI
// extension directories. It is called by SysExtPostInstall, with the EFI
// partition already mounted and both directories already created.
//
// The download goes to a temporary directory first: the same image has to end
// up in both the active and the passive directory, and pulling it twice would
// double the transfer for no gain.
func installDeclaredExtensionsToEFI(c sdkConfig.Config, targets ...string) error {
	if c.Install == nil || len(c.Install.Extensions) == 0 {
		return nil
	}
	c.Logger.Logger.Info().Int("extensions", len(c.Install.Extensions)).Msg("Installing declared extensions into the EFI partition")

	staging, err := fsutils.TempDir(c.Fs, "", "kairos-extensions-")
	if err != nil {
		return err
	}
	defer func() { _ = c.Fs.RemoveAll(staging) }()

	if err := installer.InstallDeclared(&c, c.Install.Extensions, staging); err != nil {
		return err
	}

	staged, err := c.Fs.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, entry := range staged {
		if entry.IsDir() {
			continue
		}
		for _, target := range targets {
			if err := fsutils.Copy(c.Fs, filepath.Join(staging, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
				return fmt.Errorf("copying extension %s to %s: %w", entry.Name(), target, err)
			}
			c.Logger.Debugf("copied %s to %s", entry.Name(), target)
		}
	}
	return nil
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
