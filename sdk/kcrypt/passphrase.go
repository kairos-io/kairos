package kcrypt

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/kcrypt/bus"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/lookup"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

const (
	// PassphraseMethod is the kcrypt.method value that selects
	// PassphraseEncryptor: the partitions are encrypted with a passphrase a
	// person chose, and unlocked by asking for it on the console at boot.
	PassphraseMethod = "passphrase"

	// PassphraseSubsystem is written to the LUKS2 header of a partition
	// PassphraseEncryptor creates. It tells anything that looks at the disk
	// before the configuration is readable (an initramfs hook, for example)
	// that only a person can open this container.
	PassphraseSubsystem = "kcrypt:passphrase"

	// DefaultPassphraseFallbackTimeout bounds the passphrase prompt that
	// kcrypt.passphrase_fallback shows when the automatic unlock fails, so a
	// machine nobody is looking at still reaches its failure screen.
	DefaultPassphraseFallbackTimeout = 5 * time.Minute

	// minPassphraseLength is the shortest passphrase Encrypt accepts.
	minPassphraseLength = 8
)

// passphraseFormatArgs are the luksFormat arguments for a container that a
// person unlocks by typing. They are appended after luksFormatArgs, and
// cryptsetup keeps the last value of a repeated option, so --iter-time here
// replaces the 5ms of the default arguments. That default is right for a
// random 32 character key and far too weak for a passphrase a person can
// remember: argon2id with two seconds of work is what cryptsetup itself
// picks for an interactive passphrase.
var passphraseFormatArgs = []string{
	"--pbkdf", "argon2id",
	"--iter-time", "2000",
	"--subsystem", PassphraseSubsystem,
}

// AskPasswordCommand is the program kcrypt runs to ask for a passphrase.
// systemd-ask-password hands the question to whatever password agent is
// running (systemd-tty-ask-password-agent on the console in a dracut
// initramfs, plymouth when it is there) and prints the answer on stdout. It
// is a variable so that an image can point it at its own program with the
// same interface.
var AskPasswordCommand = "systemd-ask-password"

// askPassword asks for a passphrase. timeout 0 waits forever. It is a
// variable so tests can answer without a console.
var askPassword = func(prompt, id string, timeout time.Duration) (string, error) {
	seconds := int(timeout.Round(time.Second) / time.Second)
	if timeout > 0 && seconds == 0 {
		seconds = 1
	}
	// --no-tty: immucore runs with no terminal, so the question has to go
	// through the agent that owns the console.
	cmd := exec.Command(AskPasswordCommand, "--no-tty", "--timeout="+strconv.Itoa(seconds), "--id="+id, prompt)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", AskPasswordCommand, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// openWithPassphrase opens device as /dev/mapper/<mapper>. It runs
// cryptsetup, not luks.go: cryptsetup tries every keyslot, so it also opens a
// container whose passphrase was added next to the TPM key in another slot,
// and it uses the kernel's argon2 code. It is a variable for the tests.
var openWithPassphrase = func(device, mapper, passphrase string) error {
	cmd := exec.Command("cryptsetup", "open", "--key-file=-", device, mapper)
	cmd.Stdin = strings.NewReader(passphrase)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cryptsetup open %s: %w: %s", device, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// mapperCreated reports whether the unlock created the mapper device. A
// variable for the tests, which cannot create /dev/mapper nodes.
var mapperCreated = func(p *partitions.Partition) bool {
	return !partitionLocked(p)
}

// errNoPassphrase means nobody answered the prompt before it timed out.
var errNoPassphrase = errors.New("no passphrase was entered")

// unlockByPrompt asks for the passphrase of the partition labelled label
// until it opens the partition. timeout is the total time to keep asking; 0
// asks forever.
func unlockByPrompt(label string, timeout time.Duration, logger sdkLogger.KairosLogger) error {
	info, err := findPartitionByLabel(label)
	if err != nil {
		return fmt.Errorf("find partition %s: %w", label, err)
	}
	if !partitionLocked(info) {
		return nil
	}

	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	prompt := fmt.Sprintf("Enter the passphrase for %s (%s):", label, info.Path)
	for attempt := 1; ; attempt++ {
		var left time.Duration
		if !deadline.IsZero() {
			left = time.Until(deadline)
			if left <= 0 {
				return errNoPassphrase
			}
		}

		pass, err := askPassword(prompt, "kcrypt:"+info.Name, left)
		if err != nil {
			logger.Logger.Warn().Err(err).Str("partition", label).Msg("No passphrase entered")
			return errNoPassphrase
		}

		if err := openWithPassphrase(info.Path, info.Name, pass); err != nil {
			logger.Logger.Warn().Str("partition", label).Int("attempt", attempt).Msg("Wrong passphrase")
			prompt = fmt.Sprintf("Wrong passphrase. Try again (%s):", info.Path)
			continue
		}

		if err := UdevAdmTriggerSettle(&logger, udevTimeout); err != nil {
			logger.Logger.Warn().Err(err).Str("partition", label).Msg("UdevAdmTriggerSettle failed")
		}
		if !mapperCreated(info) {
			return fmt.Errorf("unlock reported success but %s was not created", lookup.MapperPath(info))
		}
		logger.Logger.Info().Str("partition", label).Int("attempt", attempt).Msg("Unlocked with the passphrase")
		return nil
	}
}

// unlockWithPassphrase opens the partition labelled label with a passphrase
// that is already known, without asking.
func unlockWithPassphrase(label, passphrase string, logger sdkLogger.KairosLogger) error {
	info, err := findPartitionByLabel(label)
	if err != nil {
		return fmt.Errorf("find partition %s: %w", label, err)
	}
	if !partitionLocked(info) {
		return nil
	}
	if err := openWithPassphrase(info.Path, info.Name, passphrase); err != nil {
		return err
	}
	if err := UdevAdmTriggerSettle(&logger, udevTimeout); err != nil {
		logger.Logger.Warn().Err(err).Str("partition", label).Msg("UdevAdmTriggerSettle failed")
	}
	if !mapperCreated(info) {
		return fmt.Errorf("unlock reported success but %s was not created", lookup.MapperPath(info))
	}
	return nil
}

// PassphraseEncryptor encrypts partitions with a passphrase chosen by a
// person, and unlocks them by asking for it. It needs no TPM and no key
// server: it is classic full disk encryption. Selected with
// kcrypt.method: passphrase.
type PassphraseEncryptor struct {
	logger       sdkLogger.KairosLogger
	kcryptConfig *bus.KcryptConfig
}

// Encrypt reads the passphrase from kcrypt.passphrase_file. The passphrase
// is never part of the configuration itself, because the install
// configuration is saved to the unencrypted OEM partition.
func (e *PassphraseEncryptor) Encrypt(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Encrypting partitions")

	passphrase, err := readPassphraseFile(e.kcryptConfig)
	if err != nil {
		return err
	}

	for _, partition := range partitions {
		if _, err := luksifyWithPassphrase(partition, passphrase, e.logger, passphraseFormatArgs...); err != nil {
			return fmt.Errorf("failed to encrypt partition %s: %w", partition, err)
		}
	}
	return nil
}

// Unlock asks for the passphrase and waits for as long as it takes: nothing
// else can open these partitions. When kcrypt.passphrase_file can still be
// read, which is the case right after Encrypt during the install, it uses
// that instead of asking the person who just typed it into the installer.
func (e *PassphraseEncryptor) Unlock(partitions []string) error {
	e.logger.Logger.Info().Str("method", e.Name()).Strs("partitions", partitions).Msg("Unlocking encrypted partitions")
	known, _ := readPassphraseFile(e.kcryptConfig)
	for _, label := range partitions {
		if known != "" {
			err := unlockWithPassphrase(label, known, e.logger)
			if err == nil {
				continue
			}
			e.logger.Logger.Warn().Err(err).Str("partition", label).Msg("The passphrase file did not unlock the partition, asking instead")
		}
		if err := unlockByPrompt(label, 0, e.logger); err != nil {
			return fmt.Errorf("failed to unlock partition %s: %w", label, err)
		}
	}
	return nil
}

func (e *PassphraseEncryptor) Name() string {
	return "Passphrase"
}

// Validate has nothing to check: the passphrase is only needed by Encrypt,
// which reports a missing file itself, and Unlock needs no hardware.
func (e *PassphraseEncryptor) Validate() error {
	return nil
}

// CheckPassphraseFile reports why Encrypt could not read the passphrase, or
// nil when it can. Validate cannot do this check, because the same encryptor
// is chosen at boot where the file is gone; a caller that is about to
// destroy data before it encrypts (a reset) calls this first.
func (e *PassphraseEncryptor) CheckPassphraseFile() error {
	_, err := readPassphraseFile(e.kcryptConfig)
	return err
}

func readPassphraseFile(config *bus.KcryptConfig) (string, error) {
	if config == nil || config.PassphraseFile == "" {
		return "", errors.New("kcrypt.method is passphrase but kcrypt.passphrase_file is not set")
	}
	data, err := os.ReadFile(config.PassphraseFile)
	if err != nil {
		return "", fmt.Errorf("reading kcrypt.passphrase_file: %w", err)
	}
	passphrase := strings.TrimRight(string(data), "\r\n")
	if len(passphrase) < minPassphraseLength {
		return "", fmt.Errorf("the passphrase in kcrypt.passphrase_file is shorter than %d characters", minPassphraseLength)
	}
	return passphrase, nil
}

// passphraseFallbackTimeout is how long the fallback prompt waits, or 0 when
// the fallback is off.
func passphraseFallbackTimeout(config *bus.KcryptConfig) time.Duration {
	if config == nil || !config.PassphraseFallback {
		return 0
	}
	if config.PassphraseTimeout > 0 {
		return time.Duration(config.PassphraseTimeout) * time.Second
	}
	return DefaultPassphraseFallbackTimeout
}
