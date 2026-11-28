package hook

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/agent/pkg/utils"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	install "github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkSpec "github.com/kairos-io/kairos/v4/sdk/types/spec"
)

// errOEMUnavailable marks the failures that mean the hook never reached the
// COS_OEM partition at all: the label does not resolve, or the mount of it
// fails. Nothing is written when one of these comes back.
//
// The install path treats it as fatal, because an install that cannot reach
// the partition it has just created has nothing to fall back on. The first
// boot does not, see GrubFirstBootOptions.Run.
var errOEMUnavailable = errors.New("the OEM partition is not available")

// isUkiBoot reports whether this boot is a UKI boot. It reads the kernel
// command line through the config filesystem, so both grub options hooks can
// be exercised without a real /proc/cmdline, and falls back to the host read
// for a config that was built without a filesystem.
func isUkiBoot(c sdkConfig.Config) bool {
	if c.Fs == nil {
		return utils.IsUki()
	}
	return utils.IsUkiWithFs(c.Fs)
}

// GrubPostInstallOptions is a hook that runs after the install process to add grub options.
type GrubPostInstallOptions struct{}

func (b GrubPostInstallOptions) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if isUkiBoot(c) {
		c.Logger.Logger.Info().Msg("Skipping GrubPostInstallOptions hook in uki mode")
		return nil
	}

	if c.Install == nil {
		return nil
	}

	// Combine regular grub options with extracted kcrypt options
	grubOpts := make(map[string]string)

	// Copy existing grub options
	for k, v := range c.Install.GrubOptions {
		grubOpts[k] = v
	}

	// Warn on selinux configs that are set but not effective, so the
	// install log is not silent about SELinux staying off or falling
	// back to permissive.
	if c.Install.Selinux.Mode != "" && !c.Install.Selinux.Enabled {
		c.Logger.Logger.Warn().Msg("install.selinux.mode is set but install.selinux.enabled is false; SELinux stays disabled")
	}
	if c.Install.Selinux.Enabled && c.Install.Selinux.Mode != "" && !isSelinuxModeExists(c.Install.Selinux.Mode) {
		c.Logger.Logger.Warn().Msgf("unknown install.selinux.mode %q, using permissive", c.Install.Selinux.Mode)
	}

	for k, v := range SelinuxGrubOpts(c.Install.Selinux) {
		grubOpts[k] = v
	}

	// Check if COS_OEM is in the list of encrypted partitions
	oemEncrypted := false
	if len(c.Install.Encrypt) > 0 {
		for _, part := range c.Install.Encrypt {
			if part == cnst.OEMLabel {
				oemEncrypted = true
				break
			}
		}
	}

	// Extract and add kcrypt.challenger settings to cmdline if COS_OEM is encrypted
	// This solves the chicken-egg problem where kcrypt config is on the encrypted OEM partition
	// Only works in non-UKI case. For UKI, it's up to the user to add the challenger
	// server url in the cmdline when creating the signed artifact (cmdline is also signed)
	// TODO: There are more things written in the OEM partition to make boot assessment
	// and next-boot selection work. These are not handled at all right now. We should
	// fix those before we officially support OEM encryption.
	if oemEncrypted {
		c.Logger.Logger.Info().Msg("COS_OEM is encrypted, extracting kcrypt.challenger config to cmdline")
		kcryptCmdline := extractKcryptCmdline(&c)
		if kcryptCmdline != "" {
			// Append to extra_cmdline
			if existing, ok := grubOpts["extra_cmdline"]; ok {
				grubOpts["extra_cmdline"] = existing + " " + kcryptCmdline
			} else {
				grubOpts["extra_cmdline"] = kcryptCmdline
			}
			c.Logger.Logger.Info().Str("kcrypt_cmdline", kcryptCmdline).Msg("Added kcrypt config to cmdline")
		}
	}

	if len(grubOpts) == 0 {
		return nil
	}

	c.Logger.Logger.Info().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", grubOpts)
	err := grubOptions(c, grubOpts, oemEncrypted)
	if err != nil {
		return err
	}
	c.Logger.Logger.Info().Msg("Finish GrubOptions hook")
	return nil
}

// SelinuxGrubOpts builds the dedicated grubenv variable pair for
// install.selinux: selinux_enabled gates the relabel unit and the
// cmdline, selinux_mode (enforcing|permissive) carries the mode the
// relabel units setenforce call uses. Permissive is the default.
func SelinuxGrubOpts(selinux install.SelinuxOptions) map[string]string {
	if !selinux.Enabled {
		return nil
	}

	mode := selinux.Mode
	if mode == "" {
		mode = "permissive"
	}

	if !isSelinuxModeExists(mode) {
		mode = "permissive"
	}

	return map[string]string{
		"selinux_enabled": "true",
		"selinux_mode":    mode,
	}
}

// OEMIsEncrypted reports whether the partition labelled COS_OEM is a LUKS
// container on this node. It is the runtime equivalent of the install hook's
// read of install.encrypted_partitions: by the time the first boot runs, that
// config is no longer the authority on what is on the disk, so the block
// devices are asked instead. The classification is the one every other
// consumer of the question uses (the kcrypt encrypt subcommand, immucore's
// encrypt-pending step), so none of them drifts on the edge cases.
func OEMIsEncrypted() (bool, error) {
	disks, err := lookup.ScanBlockDevices()
	if err != nil {
		return false, err
	}
	return lookup.LabelIsEncrypted(disks, cnst.OEMLabel, lookup.FindByBlkid, lookup.FilesystemType)
}

// GrubFirstBootOptions is a hook that runs on the first boot to add grub options.
type GrubFirstBootOptions struct{}

func (b GrubFirstBootOptions) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if len(c.GrubOptions) == 0 {
		return nil
	}
	// A UKI boot takes its command line from the signed .cmdline section of
	// the image, so a grubenv written here is read by nobody. Skip for the
	// same reason GrubPostInstallOptions does, and say so, because the key
	// was set on purpose and the user needs to know it did not take effect.
	if isUkiBoot(c) {
		c.Logger.Logger.Warn().Msg("Skipping GrubFirstBootOptions hook in uki mode: grub_options writes a grubenv that a UKI boot never reads, the kernel command line comes from the signed .cmdline section of the image")
		return nil
	}
	c.Logger.Logger.Info().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", c.GrubOptions)

	// GRUB finds its environment block by searching the filesystems it can
	// read, so a grubenv on an encrypted COS_OEM is loaded by nobody. These
	// are the top-level grub_options, which the install hook never sees, so
	// writing them to OEM on an encrypted node loses them for good. Ask the
	// disk which grubenv GRUB will be able to read.
	oemEncrypted, err := OEMIsEncrypted()
	if err != nil {
		// Keep the previous target rather than move the file on a guess, and
		// say why the question went unanswered, because on an encrypted node
		// the options are about to be written where GRUB cannot read them.
		c.Logger.Logger.Warn().Err(err).Msg("Could not determine whether COS_OEM is encrypted, writing grub options to OEM; if it is encrypted GRUB cannot read them")
		oemEncrypted = false
	}

	if err := grubOptions(c, c.GrubOptions, oemEncrypted); err != nil {
		if errors.Is(err, errOEMUnavailable) {
			// The agent service also runs on live and netboot nodes, where
			// /oem holds the datasource userdata and there is no COS_OEM to
			// write to. Nothing was written, so the protection against
			// putting the options onto the ephemeral root still holds, and
			// there is no broken partition to shout about.
			c.Logger.Logger.Warn().Err(err).Msg("Could not reach the OEM partition, leaving the grub options unwritten")
			return nil
		}
		// Logged, not returned, for the reason FirstBootStage gives: this
		// hook runs inside the sentinel-gated first boot block, so returning
		// here aborts the chain before the firstboot sentinel is written and
		// before the bootstrap event that starts the k3s or k0s provider. The
		// node would then never finish its first boot and would repeat the
		// whole block, bundles included, on every restart of the agent unit.
		// An unwritten grubenv is worth a loud log, not that.
		c.Logger.Logger.Err(err).Msg("Failed setting grub options, continuing so the firstboot sentinel still gets written")
		return nil
	}
	c.Logger.Logger.Info().Msg("Finish GrubOptions hook")
	return nil
}

// writeGrubenvToState writes grub options to STATE partition's grubenv file
// Used when OEM is encrypted since GRUB can't read the OEM partition before decryption
//
// On a booted node the initramfs has already mounted COS_STATE read-only at
// /run/initramfs/cos-state. Mounting the same device again at
// /run/cos/state yields a second read-only mount of the same superblock, so
// the write fails with EROFS. Remount read-write for the write and put the
// flag back, which is what SelectBootEntry does for this same file.
func writeGrubenvToState(c sdkConfig.Config, opts map[string]string) error {
	device, err := lookup.MountSourceForLabel(cnst.StateLabel)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Str("label", cnst.StateLabel).Msg("Failed to find the STATE partition")
		return err
	}

	if err := fsutils.MkdirAll(c.Fs, cnst.StateDir, cnst.DirPerm); err != nil {
		c.Logger.Logger.Error().Err(err).Str("dir", cnst.StateDir).Msg("Failed to create the STATE mountpoint")
		return err
	}

	c.Logger.Logger.Debug().Str("device", device).Msg("Mounting STATE partition")
	_ = c.Mounter.Unmount(cnst.StateDir)
	if err := c.Mounter.Mount(device, cnst.StateDir, "auto", []string{}); err != nil {
		c.Logger.Logger.Error().Err(err).Str("device", device).Msg("Failed to mount the STATE partition")
		return err
	}
	defer func() {
		c.Logger.Logger.Debug().Msg("Unmounting STATE partition")
		// Back to read-only first: the remount below changes the superblock,
		// so it is also the one the initramfs mount of this device sees.
		// Say so when it fails, because STATE then stays writable for the
		// rest of the boot and nothing else reports that.
		if err := c.Mounter.Mount(device, cnst.StateDir, "auto", []string{"remount", "ro"}); err != nil {
			c.Logger.Logger.Warn().Err(err).Msg("Could not remount the STATE partition read-only, it stays writable for the rest of this boot")
		}
		_ = c.Mounter.Unmount(cnst.StateDir)
	}()

	// Best effort: a remount that fails on a filesystem that is writable
	// anyway must not stop the write, and the write reports the real error.
	if err := c.Mounter.Mount(device, cnst.StateDir, "auto", []string{"remount", "rw"}); err != nil {
		c.Logger.Logger.Warn().Err(err).Msg("Could not remount the STATE partition read-write")
	}

	grubenvPath := filepath.Join(cnst.StateDir, cnst.GrubEnv)
	err = utils.SetPersistentVariables(grubenvPath, opts, &c)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Str("grubfile", grubenvPath).Msg("Failed to set grub options in STATE")
		return err
	}
	c.Logger.Logger.Info().Str("grubfile", grubenvPath).Msg("Successfully set grub options in STATE")
	return nil
}

// oemIsMounted reports whether the COS_OEM partition is already mounted at
// /oem.
//
// The grub options hook runs in two worlds. At install time nothing has
// mounted /oem and the hook owns the mount. On the first boot of an installed
// node immucore has already mounted COS_OEM there for the rest of the boot to
// read.
//
// The mount source has to match, not just the path. A live medium and an
// install from an already running node onto a second disk both have some
// other filesystem at /oem, and writing the target's grub options through it
// would put them on the wrong node. immucore resolves the label with the same
// lookup.MountSourceForLabel this hook uses, so on the first boot the two
// device paths are the same string.
func oemIsMounted(c sdkConfig.Config, device string) (bool, error) {
	mounts, err := c.Mounter.List()
	if err != nil {
		return false, err
	}
	for _, m := range mounts {
		if m.Path == cnst.OEMPath && m.Device == device {
			return true, nil
		}
	}
	return false, nil
}

// writeGrubenvToOem writes grub options to OEM partition's grubenv file
// Used when OEM is not encrypted to avoid having two grubenv files
//
// It leaves the mount table the way it found it. Unmounting /oem on the first
// boot left the rest of the boot without the user's cloud-config: the
// first-boot cloud-config stage runs after this hook, and so does the
// config.Scan that `agent run` does once the hooks are done, and both read
// /oem. Nothing said a word about it, because every mount call here used to
// discard its result (kairos-io/kairos#5168).
func writeGrubenvToOem(c sdkConfig.Config, opts map[string]string) error {
	device, err := lookup.MountSourceForLabel(cnst.OEMLabel)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Str("label", cnst.OEMLabel).Msg("Failed to find the OEM partition")
		return fmt.Errorf("%w: %w", errOEMUnavailable, err)
	}

	mounted, err := oemIsMounted(c, device)
	if err != nil {
		// An unreadable mount table is not a reason to write the options
		// into whatever /oem happens to be, so keep taking the mount over,
		// which is what this function has always done, and say why.
		c.Logger.Logger.Warn().Err(err).Msg("Could not read the mount table, mounting OEM for the write")
		mounted = false
	}

	if mounted {
		c.Logger.Logger.Debug().Str("path", cnst.OEMPath).Str("device", device).Msg("OEM is already mounted, writing through the existing mount")
	} else {
		if err := fsutils.MkdirAll(c.Fs, cnst.OEMPath, cnst.DirPerm); err != nil {
			c.Logger.Logger.Error().Err(err).Str("dir", cnst.OEMPath).Msg("Failed to create the OEM mountpoint")
			return err
		}

		// At install time elemental has the partition at /run/cos/oem.
		_ = c.Mounter.Unmount(cnst.OEMDir)

		c.Logger.Logger.Debug().Str("device", device).Msg("Mounting OEM partition")
		if err := c.Mounter.Mount(device, cnst.OEMPath, "auto", []string{}); err != nil {
			// Writing on without the mount would put the options on the
			// ephemeral root and report success.
			c.Logger.Logger.Error().Err(err).Str("device", device).Msg("Failed to mount the OEM partition")
			return fmt.Errorf("%w: %w", errOEMUnavailable, err)
		}
		defer func() {
			c.Logger.Logger.Debug().Msg("Unmounting OEM partition")
			_ = c.Mounter.Unmount(cnst.OEMPath)
		}()
	}

	grubenvPath := filepath.Join(cnst.OEMPath, cnst.GrubEnv)
	if err := utils.SetPersistentVariables(grubenvPath, opts, &c); err != nil {
		c.Logger.Logger.Error().Err(err).Str("grubfile", grubenvPath).Msg("Failed to set grub options in OEM")
		return err
	}
	c.Logger.Logger.Info().Str("grubfile", grubenvPath).Msg("Successfully set grub options in OEM")
	return nil
}

// grubOptions sets the grub options in the grubenv file
// When OEM is not encrypted: only writes to OEM partition (grubenv) to avoid having two grubenv files
// When OEM is encrypted: only writes to STATE partition (grubenv) since GRUB can't read OEM before decryption
func grubOptions(c sdkConfig.Config, opts map[string]string, oemEncrypted bool) error {
	if oemEncrypted {
		c.Logger.Logger.Info().Msg("OEM is encrypted, writing to STATE grubenv")
		return writeGrubenvToState(c, opts)
	}
	c.Logger.Logger.Info().Msg("OEM is not encrypted, writing to OEM grubenv")
	return writeGrubenvToOem(c, opts)
}

// extractKcryptCmdline extracts the kcrypt config from the Kairos config and
// formats it as kernel command line arguments for use in grub.
// This allows kcrypt to read its settings even when COS_OEM is encrypted and
// the configuration files on it cannot be read yet.
//
// Each setting is written under the key its reader asks for.
// extractKcryptConfigFromCollector in sdk/kcrypt takes challenger_server,
// mdns and certificate from kcrypt.challenger, and nv_index, c_index and
// tpm_device from the top level of the kcrypt block, which is where
// schema.KcryptSchema declares them. Writing the TPM settings under
// kcrypt.challenger instead puts them where nothing reads them.
func extractKcryptCmdline(c *sdkConfig.Config) string {
	var cmdlineArgs []string

	// Access the generic config values map to get kcrypt settings
	if c.Collector.Values == nil {
		return ""
	}

	kcryptVal, hasKcrypt := c.Collector.Values["kcrypt"]
	if !hasKcrypt {
		return ""
	}

	// Type assert to access nested structure
	kcryptMap, ok := kcryptVal.(collector.ConfigValues)
	if !ok {
		c.Logger.Logger.Debug().Msg("kcrypt config is not in expected format")
		return ""
	}

	// The challenger block is optional. A node that unlocks against its own
	// TPM has no key management server and still needs the TPM settings
	// below, so an absent challenger block is not a reason to stop.
	if challengerVal, hasChallenger := kcryptMap["challenger"]; hasChallenger {
		challengerMap, ok := challengerVal.(collector.ConfigValues)
		if !ok {
			c.Logger.Logger.Debug().Msg("kcrypt.challenger config is not in expected format")
		} else {
			if server, ok := challengerMap["challenger_server"].(string); ok && server != "" {
				// URL encode any special characters in the server URL
				cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kcrypt.challenger.challenger_server=%s", server))
			}

			if mdns, ok := challengerMap["mdns"].(bool); ok && mdns {
				cmdlineArgs = append(cmdlineArgs, "kcrypt.challenger.mdns=true")
			}

			if cert, ok := challengerMap["certificate"].(string); ok && cert != "" {
				cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kcrypt.challenger.certificate=%s", cert))
			}
		}
	}

	// The local TPM settings live at the top level of the kcrypt block, in
	// the order the schema declares them, so the cmdline is deterministic.
	for _, key := range []string{"nv_index", "c_index", "tpm_device"} {
		if value, ok := kcryptMap[key].(string); ok && value != "" {
			cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kcrypt.%s=%s", key, value))
		}
	}

	return strings.Join(cmdlineArgs, " ")
}

func isSelinuxModeExists(m string) bool {
	availableModes := map[string]struct{}{
		"permissive": {},
		"enforcing":  {},
	}

	_, ok := availableModes[m]
	return ok
}
