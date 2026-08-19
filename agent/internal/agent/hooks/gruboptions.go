package hook

import (
	"fmt"
	"strings"

	"path/filepath"

	cnst "github.com/kairos-io/kairos-agent/v2/pkg/constants"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/machine"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkSpec "github.com/kairos-io/kairos-sdk/types/spec"
)

// GrubPostInstallOptions is a hook that runs after the install process to add grub options.
type GrubPostInstallOptions struct{}

func (b GrubPostInstallOptions) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if utils.IsUki() {
		c.Logger.Logger.Info().Msg("Skipping GrubPostInstallOptions hook in uki mode")
		return nil
	}

	// Combine regular grub options with extracted kcrypt options
	grubOpts := make(map[string]string)

	// Copy existing grub options
	for k, v := range c.Install.GrubOptions {
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

// GrubFirstBootOptions is a hook that runs on the first boot to add grub options.
type GrubFirstBootOptions struct{}

func (b GrubFirstBootOptions) Run(c sdkConfig.Config, _ sdkSpec.Spec) error {
	if len(c.GrubOptions) == 0 {
		return nil
	}
	c.Logger.Logger.Info().Msg("Running GrubOptions hook")
	c.Logger.Debugf("Setting grub options: %s", c.GrubOptions)
	// At first boot, we don't know if OEM is encrypted, so assume it's not encrypted
	// and write to OEM only (if OEM is actually encrypted, grubenv will be written to STATE during install)
	err := grubOptions(c, c.GrubOptions, false)
	if err != nil {
		return err
	}
	c.Logger.Logger.Info().Msg("Finish GrubOptions hook")
	return nil
}

// writeGrubenvToState writes grub options to STATE partition's grubenv file
// Used when OEM is encrypted since GRUB can't read the OEM partition before decryption
func writeGrubenvToState(c sdkConfig.Config, opts map[string]string) error {
	_ = machine.Umount(cnst.StateDir)
	c.Logger.Logger.Debug().Msg("Mounting STATE partition")
	_ = machine.Mount(cnst.StateLabel, cnst.StateDir)
	defer func() {
		c.Logger.Logger.Debug().Msg("Unmounting STATE partition")
		_ = machine.Umount(cnst.StateDir)
	}()

	grubenvPath := filepath.Join(cnst.StateDir, cnst.GrubEnv)
	err := utils.SetPersistentVariables(grubenvPath, opts, &c)
	if err != nil {
		c.Logger.Logger.Error().Err(err).Str("grubfile", grubenvPath).Msg("Failed to set grub options in STATE")
		return err
	}
	c.Logger.Logger.Info().Str("grubfile", grubenvPath).Msg("Successfully set grub options in STATE")
	return nil
}

// writeGrubenvToOem writes grub options to OEM partition's grubenv file
// Used when OEM is not encrypted to avoid having two grubenv files
func writeGrubenvToOem(c sdkConfig.Config, opts map[string]string) error {
	_ = machine.Umount(cnst.OEMDir)
	_ = machine.Umount(cnst.OEMPath)

	c.Logger.Logger.Debug().Msg("Mounting OEM partition")
	_ = machine.Mount(cnst.OEMLabel, cnst.OEMPath)
	defer func() {
		_ = machine.Umount(cnst.OEMPath)
	}()

	grubenvPath := filepath.Join(cnst.OEMPath, cnst.GrubEnv)
	err := utils.SetPersistentVariables(grubenvPath, opts, &c)
	if err != nil {
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

// extractKcryptCmdline extracts kcrypt.challenger config from the Kairos config and
// formats it as kernel command line arguments for use in grub.
// This allows kcrypt-challenger to access KMS settings even when COS_OEM is encrypted.
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

	challengerVal, hasChallengerKey := kcryptMap["challenger"]
	if !hasChallengerKey {
		return ""
	}

	challengerMap, ok := challengerVal.(collector.ConfigValues)
	if !ok {
		c.Logger.Logger.Debug().Msg("kcrypt.challenger config is not in expected format")
		return ""
	}

	// Extract individual settings and add as cmdline parameters
	// Using kcrypt.challenger.* prefix to match the expected config structure

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

	if nvIndex, ok := challengerMap["nv_index"].(string); ok && nvIndex != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kcrypt.challenger.nv_index=%s", nvIndex))
	}

	if cIndex, ok := challengerMap["c_index"].(string); ok && cIndex != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kcrypt.challenger.c_index=%s", cIndex))
	}

	if tpmDevice, ok := challengerMap["tpm_device"].(string); ok && tpmDevice != "" {
		cmdlineArgs = append(cmdlineArgs, fmt.Sprintf("kcrypt.challenger.tpm_device=%s", tpmDevice))
	}

	return strings.Join(cmdlineArgs, " ")
}
