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
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func withRecovery(t *testing.T, recovery bool) {
	t.Helper()
	old := leaveLockedAllowed
	t.Cleanup(func() { leaveLockedAllowed = old })
	leaveLockedAllowed = func() bool { return recovery }
}

// In recovery an empty answer leaves the partition locked, and the prompt
// says so.
func TestUnlockByPromptLeavesLockedOnAnEmptyAnswerInRecovery(t *testing.T) {
	withRecovery(t, true)
	f := newPromptFixture(t, "")

	err := unlockByPrompt(constants.PersistentLUKSLabel, 0, logger.NewNullLogger())
	if !errors.Is(err, ErrLeftLocked) {
		t.Fatalf("unlockByPrompt() error = %v, want ErrLeftLocked", err)
	}
	if f.opened != 0 {
		t.Fatal("nothing should have been opened")
	}
	if !strings.Contains(f.prompts[0], "leave the encrypted files locked: a reset will erase them") {
		t.Fatalf("prompt = %q, want the way out", f.prompts[0])
	}
}

// A cancelled question (Ctrl-D on the console, Esc in a graphical agent) is
// the same as an empty answer in recovery.
func TestUnlockByPromptLeavesLockedOnACancelInRecovery(t *testing.T) {
	withRecovery(t, true)
	newPromptFixture(t) // no answers: the fake agent fails, like a cancel

	if err := unlockByPrompt(constants.PersistentLUKSLabel, 0, logger.NewNullLogger()); !errors.Is(err, ErrLeftLocked) {
		t.Fatalf("unlockByPrompt() error = %v, want ErrLeftLocked", err)
	}
}

// The right passphrase still opens the partition in recovery, and a wrong
// one is asked again with the way out still offered.
func TestUnlockByPromptStillUnlocksInRecovery(t *testing.T) {
	withRecovery(t, true)
	f := newPromptFixture(t, "wrong", "right passphrase")

	if err := unlockByPrompt(constants.PersistentLUKSLabel, 0, logger.NewNullLogger()); err != nil {
		t.Fatalf("unlockByPrompt() error = %v", err)
	}
	if f.opened != 1 || len(f.prompts) != 2 {
		t.Fatalf("opened %d times after %d prompts, want 1 after 2", f.opened, len(f.prompts))
	}
	if !strings.HasPrefix(f.prompts[1], "Wrong passphrase") || !strings.Contains(f.prompts[1], "a reset will erase them") {
		t.Fatalf("second prompt = %q", f.prompts[1])
	}
}

// On a normal boot nothing changes: an empty answer is a wrong passphrase,
// the prompt keeps asking, and it offers no way out.
func TestUnlockByPromptKeepsAskingOnANormalBoot(t *testing.T) {
	withRecovery(t, false)
	f := newPromptFixture(t, "", "right passphrase")

	if err := unlockByPrompt(constants.PersistentLUKSLabel, 0, logger.NewNullLogger()); err != nil {
		t.Fatalf("unlockByPrompt() error = %v", err)
	}
	if f.opened != 1 || len(f.prompts) != 2 {
		t.Fatalf("opened %d times after %d prompts, want 1 after 2", f.opened, len(f.prompts))
	}
	for _, p := range f.prompts {
		if strings.Contains(p, "locked") {
			t.Fatalf("prompt %q offers a way out on a normal boot", p)
		}
	}
	if f.prompts[0] != "Enter the passphrase for "+constants.PersistentLUKSLabel+" (/dev/disk1):" {
		t.Fatalf("normal boot prompt changed: %q", f.prompts[0])
	}
}

// PassphraseEncryptor.Unlock lets the recovery boot go on and asks nothing
// more once the person chose to leave the partitions locked.
func TestPassphraseEncryptorUnlockLeftLockedInRecovery(t *testing.T) {
	withRecovery(t, true)
	f := newPromptFixture(t, "")
	e := &PassphraseEncryptor{logger: logger.NewNullLogger()}

	if err := e.Unlock([]string{constants.PersistentLUKSLabel, constants.PersistentLUKSLabel}); err != nil {
		t.Fatalf("Unlock() error = %v, want nil so the recovery boot goes on", err)
	}
	if len(f.prompts) != 1 {
		t.Fatalf("asked %d times, want 1", len(f.prompts))
	}
}

// The Trusted Boot fallback prompt offers the same way out in recovery.
func TestUnlockAllFallbackLeftLockedInRecovery(t *testing.T) {
	withRecovery(t, true)
	f := newPromptFixture(t, "")
	withUnlockConfig(t, collector.ConfigValues{
		"kcrypt": collector.ConfigValues{"passphrase_fallback": true},
	}, &failingEncryptor{})

	if err := UnlockAllEncryptedPartitions(logger.NewNullLogger()); err != nil {
		t.Fatalf("UnlockAllEncryptedPartitions() error = %v, want nil", err)
	}
	if f.opened != 0 {
		t.Fatal("nothing should have been opened")
	}
}

func fakeNewPassword(t *testing.T, answers ...string) *[]string {
	t.Helper()
	old := askNewPassword
	t.Cleanup(func() { askNewPassword = old })
	var prompts []string
	askNewPassword = func(prompt, _ string, _ time.Duration) (string, error) {
		prompts = append(prompts, prompt)
		if len(answers) == 0 {
			return "", errors.New("timed out")
		}
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
	return &prompts
}

func TestAskNewPassphrase(t *testing.T) {
	prompts := fakeNewPassword(t, "short", "long enough one", "different one", "long enough one", "long enough one")

	got, err := AskNewPassphrase("COS_PERSISTENT", time.Minute)
	if err != nil || got != "long enough one" {
		t.Fatalf("AskNewPassphrase() = %q, %v", got, err)
	}
	p := *prompts
	if len(p) != 5 || !strings.HasPrefix(p[1], "Too short.") || !strings.HasPrefix(p[3], "The two passphrases differ.") {
		t.Fatalf("prompts = %q", p)
	}
}

func TestAskNewPassphraseFailsWhenNobodyAnswers(t *testing.T) {
	fakeNewPassword(t)
	if _, err := AskNewPassphrase("COS_PERSISTENT", time.Minute); err == nil {
		t.Fatal("AskNewPassphrase() succeeded with no answer")
	}
}

func TestAskNewPassphraseGivesUpAfterTooManyAttempts(t *testing.T) {
	var answers []string
	for i := 0; i < maxNewPassphraseAttempts; i++ {
		answers = append(answers, "short")
	}
	fakeNewPassword(t, answers...)
	if _, err := AskNewPassphrase("COS_PERSISTENT", time.Minute); err == nil {
		t.Fatal("AskNewPassphrase() succeeded")
	}
}

func TestUsePassphraseFileAndWritePassphraseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir", "pass")
	if err := WritePassphraseFile(path, "a new passphrase"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", info.Mode(), err)
	}

	c := &collector.Config{Values: collector.ConfigValues{"kcrypt": map[string]interface{}{"nv_index": "0x1500001"}}}
	if err := PassphraseFileUsable(c); err == nil {
		t.Fatal("no passphrase_file should not be usable")
	}
	UsePassphraseFile(c, path)
	kc := KcryptConfigFromCollector(c, logger.NewNullLogger())
	if kc.Method != PassphraseMethod || kc.PassphraseFile != path || kc.NVIndex != "0x1500001" {
		t.Fatalf("config = %+v", kc)
	}
	if err := PassphraseFileUsable(c); err != nil {
		t.Fatalf("PassphraseFileUsable() = %v", err)
	}

	empty := &collector.Config{}
	UsePassphraseFile(empty, path)
	if KcryptConfigFromCollector(empty, logger.NewNullLogger()).PassphraseFile != path {
		t.Fatal("UsePassphraseFile on an empty config")
	}
}
