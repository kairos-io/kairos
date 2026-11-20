package action

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

// A reset that finds a passphrase container still locked recreates it.
//
// Recovery lets a person who forgot the disk passphrase go on without it
// (kcrypt's prompt takes an empty answer there), so the reset finds
// COS_PERSISTENT as a raw LUKS container instead of its unlocked mapper.
// Nobody can open that container again, so the reset erases it: it wipes the
// old header, formats the partition, and creates a new container with a new
// passphrase, the same way an install does. The old data is gone.
//
// Only passphrase containers get this. A TPM, PCR or key server container
// whose unlock failed in recovery has a key that may come back (a TPM that
// was cleared by mistake, a key server that was down), and erasing it would
// destroy data that can still be recovered, so the preflight keeps refusing
// those.

// Seams for the specs.
var (
	resetLUKSSubsystemFn    = kcrypt.LUKSSubsystem
	resetAskNewPassphraseFn = kcrypt.AskNewPassphrase
	resetWipeSignaturesFn   = func(cfg *sdkConfig.Config, device string) error {
		out, err := cfg.Runner.Run("wipefs", "--all", device)
		if err != nil {
			return fmt.Errorf("wipefs %s: %w: %s", device, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// resetPassphraseFile is where the reset keeps a passphrase typed during
	// the reset, for kcrypt.passphrase_file. /run is a tmpfs.
	resetPassphraseFile = "/run/kairos-reset/passphrase"
	// resetNewPassphraseTimeout bounds each question for a new passphrase,
	// so an unattended reset with nobody at the console stops, with nothing
	// formatted, instead of waiting for ever.
	resetNewPassphraseTimeout = 5 * time.Minute
)

// plaintextLabelFor returns the label the filesystem inside part carries:
// COS_PERSISTENT for the container COS_PERSISTENT_LUKS, COS_OEM for
// COS_OEM_LUKS. Installs from before kairos-io/kairos#4403 gave the container
// the plain label, and a container found only by its partition name has the
// label of its role.
func plaintextLabelFor(part *partitions.Partition) string {
	switch part.FilesystemLabel {
	case sdkConstants.PersistentLUKSLabel, sdkConstants.PersistentLabel:
		return sdkConstants.PersistentLabel
	case sdkConstants.OEMLUKSLabel, sdkConstants.OEMLabel:
		return sdkConstants.OEMLabel
	}
	switch part.Name {
	case sdkConstants.PersistentPartName:
		return sdkConstants.PersistentLabel
	case sdkConstants.OEMPartName:
		return sdkConstants.OEMLabel
	}
	return ""
}

// isPassphraseContainer reports whether the locked container at part may be
// recreated: its LUKS2 header says kcrypt:passphrase, or it has no subsystem
// and the configuration selects kcrypt.method: passphrase (a container made
// by a kcrypt that did not write the subsystem yet).
func isPassphraseContainer(cfg *sdkConfig.Config, part *partitions.Partition) (bool, error) {
	subsystem, err := resetLUKSSubsystemFn(part.Path)
	if err != nil {
		return false, err
	}
	return kcrypt.PassphraseContainerFor(subsystem, &cfg.Collector), nil
}

// prepareLockedPassphraseWipe is the preflight for a raw LUKS container. It
// refuses anything but a passphrase container, makes sure a new passphrase
// is at hand (kcrypt.passphrase_file, or asked twice now), and points the
// spec and the configuration at what the format and EncryptFormattedPartition
// need: the plaintext label, an ext4 filesystem, the label in
// install.encrypted_partitions, and the passphrase file in the kcrypt block.
// Nothing is written to the disk here.
func prepareLockedPassphraseWipe(cfg *sdkConfig.Config, part *partitions.Partition) error {
	refuse := func(why string) error {
		return fmt.Errorf("reset preflight: %s resolves to the raw LUKS container %s, not its unlocked mapper; "+
			"formatting it would destroy the container and its key. %s; nothing was formatted", part.FilesystemLabel, part.Path, why)
	}

	if resetIsUkiFn() {
		return refuse("Unlock it (kairos-agent kcrypt unlock-all) and retry")
	}
	passphrase, err := isPassphraseContainer(cfg, part)
	if err != nil {
		return refuse(fmt.Sprintf("Its LUKS header could not be read (%v)", err))
	}
	if !passphrase {
		return refuse("Unlock it (kairos-agent kcrypt unlock-all) and retry")
	}
	label := plaintextLabelFor(part)
	if label == "" {
		return refuse("It is not COS_PERSISTENT or COS_OEM")
	}

	cfg.Logger.Warnf("%s (%s) is a passphrase-encrypted container that is still locked. "+
		"This reset ERASES it and everything in it, and creates a new encrypted container with a new passphrase. "+
		"The old passphrase will not open it, and the old data cannot be recovered.", label, part.Path)

	if err := ensureNewPassphrase(cfg, label); err != nil {
		return fmt.Errorf("reset preflight: %s needs a new passphrase: %w; nothing was formatted", label, err)
	}

	part.FilesystemLabel = label
	part.FS = sdkConstants.LinuxFs
	if cfg.Install == nil {
		cfg.Install = &install.Install{}
	}
	if !slices.Contains(cfg.Install.Encrypt, label) {
		cfg.Install.Encrypt = append(cfg.Install.Encrypt, label)
	}
	return nil
}

// ensureNewPassphrase leaves cfg pointing at a usable passphrase file. An
// unattended reset brings one in kcrypt.passphrase_file; otherwise the
// person at the console types the new passphrase twice. A reset that wipes
// both OEM and persistent asks once.
func ensureNewPassphrase(cfg *sdkConfig.Config, label string) error {
	if err := kcrypt.PassphraseFileUsable(&cfg.Collector); err == nil {
		kc := kcrypt.KcryptConfigFromCollector(&cfg.Collector, cfg.Logger)
		cfg.Logger.Infof("using the new passphrase from kcrypt.passphrase_file %s", kc.PassphraseFile)
		kcrypt.UsePassphraseFile(&cfg.Collector, kc.PassphraseFile)
		return nil
	}

	passphrase, err := resetAskNewPassphraseFn("the encrypted "+label, resetNewPassphraseTimeout)
	if err != nil {
		return fmt.Errorf("%w (an unattended reset reads it from kcrypt.passphrase_file)", err)
	}
	if err := kcrypt.WritePassphraseFile(resetPassphraseFile, passphrase); err != nil {
		return fmt.Errorf("keeping the new passphrase for kcrypt: %w", err)
	}
	kcrypt.UsePassphraseFile(&cfg.Collector, resetPassphraseFile)
	return nil
}

// WipeLockedContainer removes the old LUKS header from part right before the
// reset formats it. It does something only to a raw LUKS container, which
// reaches the format only when prepareLockedPassphraseWipe approved it: the
// preflight refuses every other raw container. Without it the old header's
// second copy, which the ext4 format does not overwrite, would still be
// found by blkid next to the new filesystem.
func WipeLockedContainer(cfg *sdkConfig.Config, part *partitions.Partition) error {
	if !isLockedContainer(part) {
		return nil
	}
	cfg.Logger.Warnf("erasing the locked encrypted container on %s", part.Path)
	return resetWipeSignaturesFn(cfg, part.Path)
}

// isLockedContainer reports whether part is a raw LUKS container rather than
// a plaintext filesystem or an unlocked mapper.
func isLockedContainer(part *partitions.Partition) bool {
	if part == nil || part.Path == "" || strings.HasPrefix(part.Path, "/dev/mapper/") {
		return false
	}
	fs, err := resetFsProbeFn(part.Path)
	return err == nil && fs == sdkConstants.LUKSFs
}

// removeResetPassphraseFile deletes the passphrase the reset asked for, once
// the containers that needed it exist.
func removeResetPassphraseFile() error {
	err := os.Remove(resetPassphraseFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
