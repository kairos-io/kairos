package kcrypt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt/bus"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
)

func TestExtractPassphraseConfig(t *testing.T) {
	log := logger.NewNullLogger()

	tests := []struct {
		name   string
		values collector.ConfigValues
		want   bus.KcryptConfig
	}{
		{
			name: "yaml values",
			values: collector.ConfigValues{"kcrypt": collector.ConfigValues{
				"method":              "passphrase",
				"passphrase_file":     "/run/pass",
				"passphrase_fallback": true,
				"passphrase_timeout":  120,
			}},
			want: bus.KcryptConfig{Method: PassphraseMethod, PassphraseFile: "/run/pass", PassphraseFallback: true, PassphraseTimeout: 120},
		},
		{
			name: "cmdline strings",
			values: collector.ConfigValues{"kcrypt": collector.ConfigValues{
				"method":              "Passphrase",
				"passphrase_fallback": "yes",
				"passphrase_timeout":  "60",
			}},
			want: bus.KcryptConfig{Method: PassphraseMethod, PassphraseFallback: true, PassphraseTimeout: 60},
		},
		{
			name:   "absent",
			values: collector.ConfigValues{"kcrypt": collector.ConfigValues{"nv_index": "0x1"}},
			want:   bus.KcryptConfig{NVIndex: "0x1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKcryptConfigFromCollector(collector.Config{Values: tt.values}, log)
			if *got != tt.want {
				t.Fatalf("extractKcryptConfigFromCollector() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

// A passphrase install must not need a TPM: the encryptor is chosen before
// any TPM validation runs.
func TestGetEncryptorFromConfigSelectsPassphrase(t *testing.T) {
	config := &collector.Config{Values: collector.ConfigValues{
		"kcrypt": collector.ConfigValues{
			"method":     "passphrase",
			"tpm_device": "/nonexistent/tpm",
			"challenger": collector.ConfigValues{"challenger_server": "http://kms"},
		},
	}}

	encryptor, err := GetEncryptorFromConfig(logger.NewNullLogger(), config)
	if err != nil {
		t.Fatalf("GetEncryptorFromConfig() error = %v", err)
	}
	if _, ok := encryptor.(*PassphraseEncryptor); !ok {
		t.Fatalf("GetEncryptorFromConfig() = %T, want *PassphraseEncryptor", encryptor)
	}
}

func TestReadPassphraseFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	short := filepath.Join(dir, "short")
	if err := os.WriteFile(good, []byte("correct horse\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(short, []byte("abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readPassphraseFile(&bus.KcryptConfig{}); err == nil {
		t.Fatal("expected an error when passphrase_file is not set")
	}
	if _, err := readPassphraseFile(&bus.KcryptConfig{PassphraseFile: filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if _, err := readPassphraseFile(&bus.KcryptConfig{PassphraseFile: short}); err == nil {
		t.Fatal("expected an error for a short passphrase")
	}
	got, err := readPassphraseFile(&bus.KcryptConfig{PassphraseFile: good})
	if err != nil || got != "correct horse" {
		t.Fatalf("readPassphraseFile() = %q, %v; want the passphrase without its newline", got, err)
	}
}

func TestPassphraseFallbackTimeout(t *testing.T) {
	if got := passphraseFallbackTimeout(nil); got != 0 {
		t.Fatalf("nil config: got %v, want 0", got)
	}
	if got := passphraseFallbackTimeout(&bus.KcryptConfig{PassphraseTimeout: 10}); got != 0 {
		t.Fatalf("fallback off: got %v, want 0", got)
	}
	if got := passphraseFallbackTimeout(&bus.KcryptConfig{PassphraseFallback: true}); got != DefaultPassphraseFallbackTimeout {
		t.Fatalf("default: got %v, want %v", got, DefaultPassphraseFallbackTimeout)
	}
	if got := passphraseFallbackTimeout(&bus.KcryptConfig{PassphraseFallback: true, PassphraseTimeout: 30}); got != 30*time.Second {
		t.Fatalf("configured: got %v, want 30s", got)
	}
}

// promptFixture is one locked persistent partition, a fake console that
// answers with the given passphrases in turn, and a fake cryptsetup that
// accepts only "right passphrase".
type promptFixture struct {
	answers  []string
	prompts  []string
	opened   int
	openedBy map[string]bool
}

func newPromptFixture(t *testing.T, answers ...string) *promptFixture {
	t.Helper()
	return newPromptFixtureFor(t, []partitions.Partition{{
		Name:            "disk1",
		PartitionLabel:  "persistent",
		FilesystemLabel: constants.PersistentLUKSLabel,
		FS:              "crypto_LUKS",
	}}, answers...)
}

// newPromptFixtureFor is newPromptFixture with the given LUKS partitions,
// each opened under its kernel name.
func newPromptFixtureFor(t *testing.T, parts []partitions.Partition, answers ...string) *promptFixture {
	t.Helper()
	var ghwMock mocks.GhwMock
	list := partitions.PartitionList{}
	names := map[string]bool{}
	for i := range parts {
		p := parts[i]
		list = append(list, &p)
		names[p.Name] = true
	}
	ghwMock.AddDisk(partitions.Disk{Name: "disk", Partitions: list})
	ghwMock.CreateDevices()
	t.Cleanup(ghwMock.Clean)

	f := &promptFixture{answers: answers, openedBy: map[string]bool{}}
	oldAsk, oldOpen, oldMapper, oldSub := askPassword, openWithPassphrase, mapperCreated, luksHeaderSubsystem
	t.Cleanup(func() {
		askPassword, openWithPassphrase, mapperCreated, luksHeaderSubsystem = oldAsk, oldOpen, oldMapper, oldSub
	})

	askPassword = func(prompt, _ string, _ time.Duration) (string, error) {
		f.prompts = append(f.prompts, prompt)
		if len(f.answers) == 0 {
			return "", errors.New("timed out")
		}
		answer := f.answers[0]
		f.answers = f.answers[1:]
		return answer, nil
	}
	openWithPassphrase = func(_, mapper, passphrase string) error {
		if !names[mapper] {
			t.Errorf("mapper = %q, want the kernel name of a fixture partition", mapper)
		}
		if passphrase != "right passphrase" {
			return errors.New("No key available with this passphrase")
		}
		f.opened++
		f.openedBy[mapper] = true
		return nil
	}
	mapperCreated = func(p *partitions.Partition) bool { return f.openedBy[p.Name] }
	// No header says kcrypt:passphrase unless a test says so.
	luksHeaderSubsystem = func(string) string { return "" }
	return f
}

func TestUnlockByPromptRetriesAWrongPassphrase(t *testing.T) {
	f := newPromptFixture(t, "wrong", "right passphrase")

	if err := unlockByPrompt(constants.PersistentLUKSLabel, 0, logger.NewNullLogger()); err != nil {
		t.Fatalf("unlockByPrompt() error = %v", err)
	}
	if f.opened != 1 || len(f.prompts) != 2 {
		t.Fatalf("opened %d times after %d prompts, want 1 after 2", f.opened, len(f.prompts))
	}
	if !strings.HasPrefix(f.prompts[1], "Wrong passphrase") {
		t.Fatalf("second prompt = %q, want it to say the passphrase was wrong", f.prompts[1])
	}
}

func TestUnlockByPromptGivesUpWhenNobodyAnswers(t *testing.T) {
	f := newPromptFixture(t)

	err := unlockByPrompt(constants.PersistentLUKSLabel, time.Minute, logger.NewNullLogger())
	if !errors.Is(err, errNoPassphrase) {
		t.Fatalf("unlockByPrompt() error = %v, want errNoPassphrase", err)
	}
	if f.opened != 0 {
		t.Fatal("nothing should have been opened")
	}
}

type failingEncryptor struct{ unlocked bool }

func (e *failingEncryptor) Encrypt([]string) error { return nil }
func (e *failingEncryptor) Unlock([]string) error {
	e.unlocked = true
	return errors.New("TPM has no key for this partition")
}
func (e *failingEncryptor) Name() string    { return "failing" }
func (e *failingEncryptor) Validate() error { return nil }

func withUnlockConfig(t *testing.T, values collector.ConfigValues, encryptor PartitionEncryptor) {
	t.Helper()
	oldScan, oldGet := scanUnlockConfig, getEncryptorFromConfig
	t.Cleanup(func() { scanUnlockConfig, getEncryptorFromConfig = oldScan, oldGet })
	scanUnlockConfig = func(logger.KairosLogger) (*collector.Config, error) {
		return &collector.Config{Values: values}, nil
	}
	getEncryptorFromConfig = func(logger.KairosLogger, *collector.Config) (PartitionEncryptor, error) {
		return encryptor, nil
	}
}

// The TPM was cleared: the automatic unlock fails, and with
// kcrypt.passphrase_fallback the boot asks for the recovery passphrase.
func TestUnlockAllFallsBackToThePassphrase(t *testing.T) {
	f := newPromptFixture(t, "right passphrase")
	encryptor := &failingEncryptor{}
	withUnlockConfig(t, collector.ConfigValues{
		"kcrypt": collector.ConfigValues{"passphrase_fallback": true},
	}, encryptor)

	if err := UnlockAllEncryptedPartitions(logger.NewNullLogger()); err != nil {
		t.Fatalf("UnlockAllEncryptedPartitions() error = %v", err)
	}
	if !encryptor.unlocked || f.opened != 1 {
		t.Fatalf("encryptor tried: %t, opened with the passphrase: %d; want both", encryptor.unlocked, f.opened)
	}
}

// Without the opt-in the failure is returned as before and nobody is asked.
func TestUnlockAllDoesNotAskWithoutTheFallback(t *testing.T) {
	f := newPromptFixture(t, "right passphrase")
	withUnlockConfig(t, collector.ConfigValues{}, &failingEncryptor{})

	if err := UnlockAllEncryptedPartitions(logger.NewNullLogger()); err == nil {
		t.Fatal("UnlockAllEncryptedPartitions() succeeded, want the encryptor's error")
	}
	if len(f.prompts) != 0 {
		t.Fatalf("asked %d times, want no prompt", len(f.prompts))
	}
}

// A passphrase install asks and keeps asking: PassphraseEncryptor.Unlock
// waits for as long as it takes.
func TestPassphraseEncryptorUnlock(t *testing.T) {
	f := newPromptFixture(t, "wrong", "also wrong", "right passphrase")
	e := &PassphraseEncryptor{logger: logger.NewNullLogger()}

	if err := e.Unlock([]string{constants.PersistentLUKSLabel}); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if f.opened != 1 || len(f.prompts) != 3 {
		t.Fatalf("opened %d times after %d prompts, want 1 after 3", f.opened, len(f.prompts))
	}
}

// Right after Encrypt, during the install, the passphrase file is still
// there: Unlock must use it and not ask on the console.
func TestPassphraseEncryptorUnlockUsesTheFileWithoutAsking(t *testing.T) {
	f := newPromptFixture(t)
	file := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(file, []byte("right passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &PassphraseEncryptor{logger: logger.NewNullLogger(), kcryptConfig: &bus.KcryptConfig{PassphraseFile: file}}

	if err := e.Unlock([]string{constants.PersistentLUKSLabel}); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if f.opened != 1 || len(f.prompts) != 0 {
		t.Fatalf("opened %d times after %d prompts, want 1 after none", f.opened, len(f.prompts))
	}
}

// recordingEncryptor is a TPM-like encryptor that unlocks whatever it is
// given and records it.
type recordingEncryptor struct{ got []string }

func (e *recordingEncryptor) Encrypt([]string) error { return nil }
func (e *recordingEncryptor) Unlock(labels []string) error {
	e.got = append(e.got, labels...)
	return nil
}
func (e *recordingEncryptor) Name() string    { return "TPM with PCR policy" }
func (e *recordingEncryptor) Validate() error { return nil }

func TestReadLUKS2Subsystem(t *testing.T) {
	hdr := make([]byte, 4096)
	copy(hdr, luks2Magic)
	hdr[7] = 2
	copy(hdr[luks2SubsystemOffset:], PassphraseSubsystem)
	dev := filepath.Join(t.TempDir(), "dev")
	if err := os.WriteFile(dev, hdr, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readLUKS2Subsystem(dev)
	if err != nil || got != PassphraseSubsystem {
		t.Fatalf("readLUKS2Subsystem() = %q, %v; want %q", got, err, PassphraseSubsystem)
	}

	hdr[7] = 1 // LUKS1 has no subsystem field
	if err := os.WriteFile(dev, hdr, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLUKS2Subsystem(dev); err == nil {
		t.Fatal("a LUKS1 header must not report a subsystem")
	}
}

// The configuration that said kcrypt.method: passphrase is unreadable (in
// an encrypted OEM, or gone after a reset): the header alone gets the
// partition asked for, and the selected encryptor is not used for it.
func TestUnlockAllAsksForAPassphraseHeaderWithoutConfig(t *testing.T) {
	f := newPromptFixture(t, "right passphrase")
	luksHeaderSubsystem = func(string) string { return PassphraseSubsystem }
	encryptor := &recordingEncryptor{}
	withUnlockConfig(t, collector.ConfigValues{}, encryptor)

	if err := UnlockAllEncryptedPartitions(logger.NewNullLogger()); err != nil {
		t.Fatalf("UnlockAllEncryptedPartitions() error = %v", err)
	}
	if f.opened != 1 || len(f.prompts) != 1 {
		t.Fatalf("opened %d times after %d prompts, want 1 after 1", f.opened, len(f.prompts))
	}
	if len(encryptor.got) != 0 {
		t.Fatalf("the selected encryptor was given %v, want nothing", encryptor.got)
	}
}

// A passphrase container next to a TPM container: each goes its own way.
func TestUnlockAllSplitsPassphraseAndTPMContainers(t *testing.T) {
	f := newPromptFixtureFor(t, []partitions.Partition{
		{Name: "disk1", PartitionLabel: "oem", FilesystemLabel: constants.OEMLUKSLabel, FS: "crypto_LUKS"},
		{Name: "disk2", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLUKSLabel, FS: "crypto_LUKS"},
	}, "right passphrase")
	luksHeaderSubsystem = func(label string) string {
		if label == constants.PersistentLUKSLabel {
			return PassphraseSubsystem
		}
		return ""
	}
	encryptor := &recordingEncryptor{}
	withUnlockConfig(t, collector.ConfigValues{}, encryptor)

	if err := UnlockAllEncryptedPartitions(logger.NewNullLogger()); err != nil {
		t.Fatalf("UnlockAllEncryptedPartitions() error = %v", err)
	}
	if !f.openedBy["disk2"] || f.openedBy["disk1"] || len(f.prompts) != 1 {
		t.Fatalf("opened by passphrase: %v after %d prompts; want only disk2 after 1", f.openedBy, len(f.prompts))
	}
	if len(encryptor.got) != 1 || encryptor.got[0] != constants.OEMLUKSLabel {
		t.Fatalf("the TPM encryptor was given %v, want [%s]", encryptor.got, constants.OEMLUKSLabel)
	}
}

// OEM and persistent with one passphrase: asked once.
func TestUnlockAllAsksOnceForTwoPassphraseContainers(t *testing.T) {
	f := newPromptFixtureFor(t, []partitions.Partition{
		{Name: "disk1", PartitionLabel: "oem", FilesystemLabel: constants.OEMLUKSLabel, FS: "crypto_LUKS"},
		{Name: "disk2", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLUKSLabel, FS: "crypto_LUKS"},
	}, "wrong", "right passphrase")
	luksHeaderSubsystem = func(string) string { return PassphraseSubsystem }
	encryptor := &recordingEncryptor{}
	withUnlockConfig(t, collector.ConfigValues{}, encryptor)

	if err := UnlockAllEncryptedPartitions(logger.NewNullLogger()); err != nil {
		t.Fatalf("UnlockAllEncryptedPartitions() error = %v", err)
	}
	if !f.openedBy["disk1"] || !f.openedBy["disk2"] {
		t.Fatalf("opened %v, want both", f.openedBy)
	}
	if len(f.prompts) != 2 {
		t.Fatalf("asked %d times, want 2 (one wrong, one right, none for the second partition)", len(f.prompts))
	}
	if len(encryptor.got) != 0 {
		t.Fatalf("the selected encryptor was given %v, want nothing", encryptor.got)
	}
}
