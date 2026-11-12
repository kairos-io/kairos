package kcrypt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
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
	var console *os.File
	if !systemdBooted() {
		// No systemd, so no password agent: a UKI boot, where immucore is
		// PID 1. systemd-ask-password --no-tty would queue the question for
		// an agent that never comes and time out with nothing on the
		// screen, so ask on the console itself.
		f, err := os.OpenFile(passwordConsole, os.O_RDWR, 0)
		if err != nil {
			return "", fmt.Errorf("opening %s to ask for the passphrase: %w", passwordConsole, err)
		}
		defer f.Close()
		console = f
	}
	cmd := exec.Command(AskPasswordCommand, askPasswordArgs(prompt, id, timeout, console != nil)...)
	cmd.Stderr = os.Stderr
	if console != nil {
		cmd.Stdin, cmd.Stderr = console, console
		// systemd-ask-password reads from /dev/tty, its controlling
		// terminal; a child of PID 1 has none, so make the console its own.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", AskPasswordCommand, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// passwordConsole is where the passphrase is asked when no password agent
// runs: the kernel's console, the last console= on the cmdline.
var passwordConsole = "/dev/console"

// systemdBooted reports whether systemd is PID 1, the same test as
// sd_booted(3). Only then does a password agent answer systemd-ask-password.
var systemdBooted = func() bool {
	st, err := os.Stat("/run/systemd/system")
	return err == nil && st.IsDir()
}

// askPasswordArgs are the systemd-ask-password arguments. With onConsole
// it reads from its stdin, which is the console; otherwise --no-tty hands
// the question to the agent that owns the console (dracut's
// systemd-tty-ask-password-agent, or plymouth).
func askPasswordArgs(prompt, id string, timeout time.Duration, onConsole bool) []string {
	seconds := int(timeout.Round(time.Second) / time.Second)
	if timeout > 0 && seconds == 0 {
		seconds = 1
	}
	args := []string{"--timeout=" + strconv.Itoa(seconds), "--id=" + id, prompt}
	if !onConsole {
		args = append([]string{"--no-tty"}, args...)
	}
	return args
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
	_, err := promptUntilOpen(label, timeout, logger)
	return err
}

// promptUntilOpen is unlockByPrompt that also returns the passphrase that
// opened the partition, so the caller can try it on the next one before it
// asks again. It returns "" when the partition was already open.
func promptUntilOpen(label string, timeout time.Duration, logger sdkLogger.KairosLogger) (string, error) {
	info, err := findPartitionByLabel(label)
	if err != nil {
		return "", fmt.Errorf("find partition %s: %w", label, err)
	}
	if !partitionLocked(info) {
		return "", nil
	}

	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	// In recovery an empty answer leaves the partition locked, so a
	// forgotten passphrase does not keep anyone out of the reset. Only an
	// answer counts: a timeout or a failed password agent is nobody choosing
	// to have the container erased.
	leaveLocked := leaveLockedAllowed()
	withHint := func(s string) string {
		if leaveLocked {
			return leaveLockedPrompt(s)
		}
		return s
	}

	p := promptFor(luksTokenTypes(label))
	prompt := withHint(p.ask)
	for attempt := 1; ; attempt++ {
		var left time.Duration
		if !deadline.IsZero() {
			left = time.Until(deadline)
			if left <= 0 {
				return "", errNoPassphrase
			}
		}

		pass, err := askPassword(prompt, "kcrypt:"+info.Name, left)
		if leaveLocked && err == nil && pass == "" {
			logger.Logger.Warn().Str("partition", label).Msg("Left locked on request: a reset will erase it")
			return "", ErrLeftLocked
		}
		if err != nil {
			logger.Logger.Warn().Err(err).Str("partition", label).Msg("No passphrase entered")
			return "", errNoPassphrase
		}

		pass, err = openTyped(info, pass, p.recovery)
		if err != nil {
			logger.Logger.Warn().Str("partition", label).Int("attempt", attempt).Msg(p.wrong)
			prompt = withHint(p.wrong)
			continue
		}

		if err := UdevAdmTriggerSettle(&logger, udevTimeout); err != nil {
			logger.Logger.Warn().Err(err).Str("partition", label).Msg("UdevAdmTriggerSettle failed")
		}
		if !mapperCreated(info) {
			return "", fmt.Errorf("unlock reported success but %s was not created", lookup.MapperPath(info))
		}
		logger.Logger.Info().Str("partition", label).Int("attempt", attempt).Msg("Unlocked with " + p.what)
		return pass, nil
	}
}

// unlockLabelsByPrompt opens every partition in labels with a passphrase.
// It starts with the known passphrases (a passphrase file, say), and every
// passphrase that opens a partition is tried on the remaining ones before
// anyone is asked again, so OEM and persistent sharing one passphrase ask
// once. timeout bounds each prompt; 0 asks forever.
func unlockLabelsByPrompt(labels []string, timeout time.Duration, logger sdkLogger.KairosLogger, known ...string) error {
	var tried []string
	for _, k := range known {
		if k != "" {
			tried = append(tried, k)
		}
	}
	for _, label := range labels {
		opened := false
		for _, pass := range tried {
			if err := unlockWithPassphrase(label, pass, logger); err == nil {
				logger.Logger.Info().Str("partition", label).Msg("Unlocked with a passphrase entered earlier")
				opened = true
				break
			}
		}
		if opened {
			continue
		}
		pass, err := promptUntilOpen(label, timeout, logger)
		if errors.Is(err, ErrLeftLocked) {
			// The person chose to go on without the passphrase (recovery
			// only): do not ask for the other partitions, and let the
			// boot go on with all of them locked.
			logger.Logger.Warn().Strs("partitions", labels).Msg("Passphrase partitions left locked on request")
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to unlock partition %s: %w", label, err)
		}
		if pass != "" {
			tried = append(tried, pass)
		}
	}
	return nil
}

// RecoveryKeyTokenType is the LUKS2 token type that marks the keyslot of a
// recovery key. It is the type systemd-cryptenroll --recovery-key writes, so
// a key enrolled either way is recognised, and systemd-cryptenroll
// --wipe-slot=recovery removes it.
const RecoveryKeyTokenType = "systemd-recovery"

// luksTokenTypes returns the token types in the LUKS2 header of the
// container labelled label, or nil when it cannot be read. A variable for
// the tests.
var luksTokenTypes = func(label string) []string {
	info, err := findPartitionByLabel(label)
	if err != nil || info == nil || info.Path == "" {
		return nil
	}
	types, err := readLUKS2TokenTypes(info.Path)
	if err != nil {
		return nil
	}
	return types
}

// luksHasRecoveryKey reports whether the container labelled label has a
// recovery key keyslot, by its token.
func luksHasRecoveryKey(label string) bool {
	return slices.Contains(luksTokenTypes(label), RecoveryKeyTokenType)
}

// prompt is what the boot asks and answers for one container. No prompt
// names a partition: the person knows "the disk", not COS_PERSISTENT_LUKS.
type prompt struct {
	ask, wrong, what string
	recovery         bool
}

// promptFor words the prompt by what the container holds:
//   - a TPM-sealed container with a recovery key (Trusted Boot): only the
//     recovery key can be typed, and the prompt says what it looks like;
//   - a passphrase container with a recovery key: either works;
//   - anything else: the passphrase.
func promptFor(tokenTypes []string) prompt {
	recovery := slices.Contains(tokenTypes, RecoveryKeyTokenType)
	switch {
	case recovery && slices.Contains(tokenTypes, "systemd-tpm2"):
		return prompt{
			ask:      "Enter the recovery key for this computer's disk (8 groups of 8 letters; dashes optional)",
			wrong:    "Wrong recovery key, try again",
			what:     "the recovery key",
			recovery: true,
		}
	case recovery:
		return prompt{
			ask:      "Enter the disk passphrase or recovery key",
			wrong:    "Wrong passphrase or recovery key, try again",
			what:     "the passphrase or recovery key",
			recovery: true,
		}
	}
	return prompt{ask: "Enter the disk passphrase", wrong: "Wrong passphrase, try again", what: "the passphrase"}
}

// openTyped opens the container with what was typed. When it may be a
// recovery key and the text as typed does not open it, the key's canonical
// form is tried too, so the key can be typed in any case and without its
// dashes while a passphrase is still used byte for byte. It returns the form
// that opened the container: that is what the next container is tried with.
func openTyped(info *partitions.Partition, typed string, recovery bool) (string, error) {
	err := openWithPassphrase(info.Path, info.Name, typed)
	if err == nil || !recovery {
		return typed, err
	}
	if key := normalizeRecoveryKey(typed); key != typed {
		return key, openWithPassphrase(info.Path, info.Name, key)
	}
	return typed, err
}

// readLUKS2TokenTypes returns the type of every token in the JSON area of
// the primary LUKS2 header on device. The JSON area starts at 4096 bytes and
// runs to hdr_size, the big-endian uint64 at offset 8.
func readLUKS2TokenTypes(device string) ([]string, error) {
	f, err := os.Open(device)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	bin := make([]byte, 16)
	if _, err := io.ReadFull(f, bin); err != nil {
		return nil, err
	}
	if !bytes.Equal(bin[:6], luks2Magic) || binary.BigEndian.Uint16(bin[6:8]) != 2 {
		return nil, fmt.Errorf("%s has no LUKS2 header", device)
	}
	size := binary.BigEndian.Uint64(bin[8:16])
	if size <= 4096 || size > 4<<20 {
		return nil, fmt.Errorf("%s: LUKS2 header size %d out of range", device, size)
	}
	area := make([]byte, size-4096)
	if _, err := f.ReadAt(area, 4096); err != nil {
		return nil, err
	}
	if i := bytes.IndexByte(area, 0); i >= 0 {
		area = area[:i]
	}
	var meta struct {
		Tokens map[string]struct {
			Type string `json:"type"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(area, &meta); err != nil {
		return nil, fmt.Errorf("%s: LUKS2 metadata: %w", device, err)
	}
	var types []string
	for _, t := range meta.Tokens {
		types = append(types, t.Type)
	}
	return types, nil
}

// modhex is the alphabet of systemd's recovery keys: 16 letters with no
// look-alikes, one per nibble.
const modhex = "cbdefghijklnrtuv"

// normalizeRecoveryKey forgives how a recovery key is typed: upper case,
// spaces, or no dashes at all. The keyslot holds the canonical form, eight
// dash-separated groups of eight modhex characters. Anything that is not 64
// modhex characters is returned as typed.
func normalizeRecoveryKey(in string) string {
	var chars []rune
	for _, r := range strings.ToLower(in) {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			continue
		case strings.ContainsRune(modhex, r):
			chars = append(chars, r)
		default:
			return in
		}
	}
	if len(chars) != 64 {
		return in
	}
	var b strings.Builder
	for i := 0; i < 64; i += 8 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(string(chars[i : i+8]))
	}
	return b.String()
}

// recoveryKeyAmong reports whether any of labels has a recovery key.
func recoveryKeyAmong(labels []string) bool {
	return slices.ContainsFunc(labels, luksHasRecoveryKey)
}

// luksHeaderSubsystem returns the LUKS2 subsystem field of the container
// labelled label, or "" when it cannot be read. A variable for the tests.
var luksHeaderSubsystem = func(label string) string {
	info, err := findPartitionByLabel(label)
	if err != nil || info == nil || info.Path == "" {
		return ""
	}
	sub, err := readLUKS2Subsystem(info.Path)
	if err != nil {
		return ""
	}
	return sub
}

// The binary LUKS2 header (cryptsetup's docs/on-disk-format-luks2.pdf):
// magic[6] version[2] hdr_size[8] seqid[8] label[48] csum_alg[32] salt[64]
// uuid[40] subsystem[48] ...
const (
	luks2SubsystemOffset = 6 + 2 + 8 + 8 + 48 + 32 + 64 + 40
	luks2SubsystemLen    = 48
)

var luks2Magic = []byte{'L', 'U', 'K', 'S', 0xba, 0xbe}

// readLUKS2Subsystem reads the subsystem string from the primary LUKS2
// header on device. It needs no cryptsetup, so it works in any initramfs.
func readLUKS2Subsystem(device string) (string, error) {
	f, err := os.Open(device)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hdr := make([]byte, luks2SubsystemOffset+luks2SubsystemLen)
	if _, err := io.ReadFull(f, hdr); err != nil {
		return "", err
	}
	if !bytes.Equal(hdr[:6], luks2Magic) || binary.BigEndian.Uint16(hdr[6:8]) != 2 {
		return "", fmt.Errorf("%s has no LUKS2 header", device)
	}
	sub := hdr[luks2SubsystemOffset:]
	if i := bytes.IndexByte(sub, 0); i >= 0 {
		sub = sub[:i]
	}
	return string(sub), nil
}

// splitPassphraseContainers separates the containers whose header says only
// a person can open them (subsystem kcrypt:passphrase) from the others. The
// header is read because the configuration that set kcrypt.method may live
// in an encrypted OEM, or be gone after a reset that formatted OEM.
func splitPassphraseContainers(labels []string) (passphrase, others []string) {
	for _, label := range labels {
		if luksHeaderSubsystem(label) == PassphraseSubsystem {
			passphrase = append(passphrase, label)
		} else {
			others = append(others, label)
		}
	}
	return passphrase, others
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
	return unlockLabelsByPrompt(partitions, 0, e.logger, known)
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
