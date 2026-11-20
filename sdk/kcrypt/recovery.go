package kcrypt

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/state"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/rs/zerolog"
)

// LeaveLockedText is what the passphrase prompt offers in recovery: an empty
// answer goes on without the passphrase. A timeout or a failed password
// agent does not, as nobody chose to have the containers erased.
const LeaveLockedText = "Leave the encrypted files locked: a reset will erase them"

// ErrLeftLocked means the person at the console chose to go on without the
// passphrase. Only a recovery or reset boot offers that.
var ErrLeftLocked = errors.New("the encrypted partitions were left locked on request")

// leaveLockedAllowed reports whether the prompt may let the boot go on with
// the passphrase partitions still locked. That is the case in recovery and
// in the automatic reset (the statereset entry), which boot the recovery
// image and do not mount COS_PERSISTENT. A forgotten passphrase must not
// keep a person out of the reset that is the only way back. A normal boot
// keeps asking: without the partition it has no /home and no /var.
//
// It reads the same command line words immucore reads to pick the boot state
// (and the EFI entry on a UKI). A variable for the tests.
var leaveLockedAllowed = func() bool {
	switch state.DetectBoot(zerolog.Nop()) {
	case state.Recovery, state.AutoReset:
		return true
	}
	return false
}

// leaveLockedPrompt adds the way out to a recovery prompt. Any password agent
// shows the text, and the console agent sends an empty answer on Enter.
func leaveLockedPrompt(prompt string) string {
	return strings.TrimSuffix(prompt, ":") + ". Press Enter with nothing typed to " + lowerFirst(LeaveLockedText) + ":"
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// LUKSSubsystem returns the LUKS2 subsystem written in the header of device,
// or "" when it has none (the TPM and key server encryptors write none). A
// device without a LUKS2 header is an error. A variable for the tests.
var LUKSSubsystem = readLUKS2Subsystem

// PassphraseContainerFor reports whether a LUKS container whose header
// carries subsystem can only be opened by a passphrase a person types: the
// header says kcrypt:passphrase, or it has no subsystem (a container made
// before kcrypt wrote one) and c selects kcrypt.method: passphrase. A reset
// may recreate such a container when it was left locked, because no machine
// key can ever open it again.
func PassphraseContainerFor(subsystem string, c *collector.Config) bool {
	if subsystem == PassphraseSubsystem {
		return true
	}
	if subsystem != "" || c == nil {
		return false
	}
	return extractKcryptConfigFromCollector(*c, sdkLogger.NewNullLogger()).Method == PassphraseMethod
}

// askNewPassword asks for a new passphrase. Unlike askPassword it does not
// pass --no-tty: a reset typed at a shell has a terminal, and
// systemd-ask-password asks on it; a reset run as a service without one goes
// through the password agents. A variable for the tests.
var askNewPassword = func(prompt, id string, timeout time.Duration) (string, error) {
	seconds := int(timeout.Round(time.Second) / time.Second)
	if timeout > 0 && seconds == 0 {
		seconds = 1
	}
	cmd := exec.Command(AskPasswordCommand, "--timeout="+strconv.Itoa(seconds), "--id="+id, prompt)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", AskPasswordCommand, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// maxNewPassphraseAttempts bounds how often AskNewPassphrase starts over
// after a short passphrase or a confirmation that does not match.
const maxNewPassphraseAttempts = 5

// AskNewPassphrase asks twice for the passphrase of a container that is
// about to be created, and returns it once both answers match and it is long
// enough. timeout bounds each question; 0 waits forever. An unanswered or
// cancelled question returns an error, and so do too many failed attempts.
func AskNewPassphrase(what string, timeout time.Duration) (string, error) {
	note := ""
	for attempt := 0; attempt < maxNewPassphraseAttempts; attempt++ {
		first, err := askNewPassword(fmt.Sprintf("%sNew passphrase for %s (at least %d characters):", note, what, minPassphraseLength), "kcrypt:new", timeout)
		if err != nil {
			return "", fmt.Errorf("no new passphrase was entered: %w", err)
		}
		if len(first) < minPassphraseLength {
			note = "Too short. "
			continue
		}
		second, err := askNewPassword(fmt.Sprintf("Type the new passphrase for %s again:", what), "kcrypt:new-confirm", timeout)
		if err != nil {
			return "", fmt.Errorf("the new passphrase was not confirmed: %w", err)
		}
		if first != second {
			note = "The two passphrases differ. "
			continue
		}
		return first, nil
	}
	return "", fmt.Errorf("no usable new passphrase after %d attempts", maxNewPassphraseAttempts)
}

// WritePassphraseFile writes passphrase to path for kcrypt.passphrase_file:
// a directory only root can enter, a file only root can read.
func WritePassphraseFile(path, passphrase string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(passphrase), 0o600); err != nil {
		return err
	}
	// WriteFile keeps the mode of a file that already exists.
	return os.Chmod(path, 0o600)
}

// UsePassphraseFile points the kcrypt configuration in c at path and selects
// the passphrase method, so every encryptor built from c afterwards encrypts
// and unlocks with that file. The rest of the kcrypt block is kept.
func UsePassphraseFile(c *collector.Config, path string) {
	if c.Values == nil {
		c.Values = collector.ConfigValues{}
	}
	block, ok := asConfigValues(c.Values["kcrypt"])
	if !ok {
		block = collector.ConfigValues{}
	}
	block["method"] = PassphraseMethod
	block["passphrase_file"] = path
	c.Values["kcrypt"] = block
}

// PassphraseFileUsable reports why the passphrase file in c cannot be used,
// or nil when it can: the check Encrypt makes before it reads it.
func PassphraseFileUsable(c *collector.Config) error {
	if c == nil {
		return errors.New("no configuration")
	}
	_, err := readPassphraseFile(extractKcryptConfigFromCollector(*c, sdkLogger.NewNullLogger()))
	return err
}
