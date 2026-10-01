package splash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplashDisabledOnlyOnTheExplicitKillSwitch(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, cmdline string
		want          bool
	}{
		// The kill switch, in the spellings a user will try.
		{"off-0", "root=LABEL=COS_STATE splash kairos.splash=0 quiet", true},
		{"off-word", "splash kairos.splash=off", true},
		{"off-false", "splash kairos.splash=false", true},
		{"off-no", "kairos.splash=no", true},
		{"splash-word", "root=LABEL=COS_STATE quiet splash loglevel=3", false},
		{"explicit-on", "kairos.splash=1", false},
		// A cmdline that says nothing about the splash is not the binary's
		// business: the unit's ConditionKernelCommandLine decides whether
		// this boot wants an animation at all.
		{"no-splash-word", "root=LABEL=COS_STATE rd.immucore.debug", false},
		{"empty", "", false},
		// A parameter that merely contains the switch is not the switch.
		{"substring", "nokairos.splash=0x", false},
		{"other-key", "foo=kairos.splash=0", false},
	} {
		p := filepath.Join(dir, tc.name)
		if err := os.WriteFile(p, []byte(tc.cmdline+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := splashDisabled(p); got != tc.want {
			t.Errorf("%s: splashDisabled(%q) = %v, want %v", tc.name, tc.cmdline, got, tc.want)
		}
	}
}

// Running `kairos splash` by hand on a machine with no /proc/cmdline, or in a
// container, must animate rather than silently exit.
func TestSplashNotDisabledWhenTheCmdlineIsUnreadable(t *testing.T) {
	if splashDisabled(filepath.Join(t.TempDir(), "absent")) {
		t.Error("an unreadable cmdline disabled the splash")
	}
}

func TestOpenConsoleFallsBackToStdout(t *testing.T) {
	out, in, cleanup, err := openConsole("")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if out != os.Stdout || in != os.Stdin {
		t.Error("an empty path did not use the process's own streams")
	}
}

func TestOpenConsoleReportsAMissingDevice(t *testing.T) {
	if _, _, _, err := openConsole(filepath.Join(t.TempDir(), "tty9")); err == nil {
		t.Error("opening a missing console succeeded")
	}
}

// A console that cannot be opened read-write is still worth animating on; only
// the Escape toggle is lost.
func TestOpenConsoleAcceptsAWriteOnlyDevice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wo")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o200)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write-only mode, so there is nothing to test")
	}
	out, in, cleanup, err := openConsole(p)
	if err != nil {
		t.Fatalf("openConsole: %v", err)
	}
	defer cleanup()
	if out == nil {
		t.Error("no writer for a write-only console")
	}
	if in != nil {
		t.Error("returned a reader for a console that cannot be read")
	}
}
