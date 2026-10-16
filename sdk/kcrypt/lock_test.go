package kcrypt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stubCryptsetup puts a cryptsetup on PATH that records its arguments and its
// standard input, so a test can see the command createLuks actually ran.
func stubCryptsetup(t *testing.T, exitCode int) (argvFile, stdinFile string) {
	t.Helper()

	dir := t.TempDir()
	argvFile = filepath.Join(dir, "argv")
	stdinFile = filepath.Join(dir, "stdin")

	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argvFile + "\n" +
		"cat > " + stdinFile + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "cryptsetup"), []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvFile, stdinFile
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// A hardened image writes /etc/security/pwquality.conf, and a cryptsetup
// linked against libpwquality then rejects the passphrase Kairos generated.
// The passphrase is not a user password, so the check must be off.
// See kairos-io/kairos#5248.
func TestCreateLuksDisablesThePasswordQualityCheck(t *testing.T) {
	argvFile, stdinFile := stubCryptsetup(t, 0)

	if err := createLuks("/dev/vda2", "a-generated-passphrase"); err != nil {
		t.Fatalf("createLuks: %v", err)
	}

	argv := readLines(t, argvFile)
	if !contains(argv, "--force-password") {
		t.Errorf("cryptsetup ran without --force-password, so a pwquality.conf can refuse the passphrase: %v", argv)
	}
	if argv[0] != "luksFormat" {
		t.Errorf("first argument = %q, want luksFormat: %v", argv[0], argv)
	}
	if !contains(argv, "/dev/vda2") {
		t.Errorf("device missing from %v", argv)
	}

	// The passphrase still goes in on stdin, never on the command line, where
	// it would show up in the process list.
	if got := string(mustRead(t, stdinFile)); got != "a-generated-passphrase" {
		t.Errorf("stdin = %q, want the passphrase", got)
	}
	for _, a := range argv {
		if strings.Contains(a, "a-generated-passphrase") {
			t.Errorf("passphrase leaked into the arguments: %v", argv)
		}
	}
}

// The callers pass PCR binding and keyfile options through, and the new flag
// must not displace them.
func TestCreateLuksKeepsTheCallersArguments(t *testing.T) {
	argvFile, _ := stubCryptsetup(t, 0)

	if err := createLuks("/dev/vda3", "pass", "--pbkdf", "pbkdf2", "--label", "COS_PERSISTENT"); err != nil {
		t.Fatalf("createLuks: %v", err)
	}

	argv := readLines(t, argvFile)
	for _, want := range []string{"--force-password", "--pbkdf", "pbkdf2", "--label", "COS_PERSISTENT", "/dev/vda3"} {
		if !contains(argv, want) {
			t.Errorf("%q missing from %v", want, argv)
		}
	}
	// cryptsetup takes the device positionally, so it has to stay ahead of the
	// options the caller appended.
	if indexOf(argv, "/dev/vda3") > indexOf(argv, "--pbkdf") {
		t.Errorf("device must come before the caller's options: %v", argv)
	}
}

func TestCreateLuksReportsAFailure(t *testing.T) {
	stubCryptsetup(t, 2)

	// Exit 2 is what a rejected passphrase returns, and it was being swallowed
	// into a bare "exit status 2" at the call site. It must still be an error.
	if err := createLuks("/dev/vda2", "pass"); err == nil {
		t.Fatal("createLuks returned nil for a cryptsetup that exited 2")
	}
}

func contains(haystack []string, needle string) bool {
	return indexOf(haystack, needle) >= 0
}

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
