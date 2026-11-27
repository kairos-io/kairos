package netbootmgr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain shortens the wait for a server that fails at start, which every
// test that starts one sits through.
func TestMain(m *testing.M) {
	startupGracePeriod = 300 * time.Millisecond
	os.Exit(m.Run())
}

// stubAurorabootFailingToBind puts an "auroraboot" first on PATH that fails
// the way pixiecore does when its port is taken.
func stubAurorabootFailingToBind(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "auroraboot")
	// The pause keeps the two lines in order: stdout and stderr are copied
	// into the log by separate goroutines.
	script := "#!/bin/sh\necho 'starting pixiecore'\nsleep 0.1\n" +
		"echo 'listen tcp 0.0.0.0:8090: bind: address already in use' >&2\nexit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestStartReportsAServerThatExitsRightAway(t *testing.T) {
	// Long enough for the stub to exit on a loaded machine. The start returns
	// as soon as it does, so the test does not wait for the whole period.
	saved := startupGracePeriod
	startupGracePeriod = 10 * time.Second
	t.Cleanup(func() { startupGracePeriod = saved })
	stubAurorabootFailingToBind(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)

	m := NewManager("", nil)
	err := m.Start(artifactsDir, artifactID)
	if !errors.Is(err, ErrExitedAtStart) {
		t.Fatalf("Start: got %v, want ErrExitedAtStart", err)
	}
	for _, want := range []string{"0.0.0.0:8090", "exit status 1", "bind: address already in use"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Start error %q does not name %q", err, want)
		}
	}
	if s := m.GetStatus(); s.Running {
		t.Errorf("Running = true after a start that failed: %+v", s)
	}

	// The failed start leaves nothing behind: the next one is attempted
	// rather than refused as already running.
	startupGracePeriod = saved
	stubAuroraboot(t)
	if err := m.Start(artifactsDir, artifactID); err != nil {
		t.Fatalf("Start after a failed start: %v", err)
	}
	defer func() { _ = m.Stop() }()
	if !m.GetStatus().Running {
		t.Error("Running = false after a successful Start")
	}
}

func TestStartListensOnTheManagerPort(t *testing.T) {
	recorded := stubAurorabootRecordingArgs(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)

	m := NewManager("", nil).WithPort("9090")
	if err := m.Start(artifactsDir, artifactID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	if got := m.GetStatus().Port; got != "9090" {
		t.Errorf("Port = %q, want 9090", got)
	}
	if got := readRecordedArgs(t, recorded); !strings.Contains(got, bindAddress+" 9090 ") {
		t.Errorf("start-pixie args = %q, want it to listen on %s 9090", got, bindAddress)
	}
}

func TestWithPortKeepsTheDefaultForAnEmptyPort(t *testing.T) {
	if got := NewManager("", nil).WithPort("").port; got != DefaultPort {
		t.Errorf("port = %q, want %q", got, DefaultPort)
	}
}

// A build's netboot_http_port reaches StartWithPaths through the deployer, and
// wins over the port of the fleet server, as it does for the CLI.
func TestStartWithPathsListensOnTheBuildPort(t *testing.T) {
	recorded := stubAurorabootRecordingArgs(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)
	netbootDir := filepath.Join(artifactsDir, artifactID, "netboot")

	m := NewManager("", nil).WithPort("9090")
	err := m.StartWithPaths(artifactID, "",
		filepath.Join(netbootDir, "kairos.squashfs"),
		filepath.Join(netbootDir, "kairos-initrd"),
		filepath.Join(netbootDir, "kairos-kernel"),
		"", "7070")
	if err != nil {
		t.Fatalf("StartWithPaths: %v", err)
	}
	defer func() { _ = m.Stop() }()

	if got := m.GetStatus().Port; got != "7070" {
		t.Errorf("Port = %q, want 7070", got)
	}
	if got := readRecordedArgs(t, recorded); !strings.Contains(got, bindAddress+" 7070 ") {
		t.Errorf("start-pixie args = %q, want it to listen on %s 7070", got, bindAddress)
	}
}

func TestValidatePort(t *testing.T) {
	for _, port := range []string{"1", "8090", "65535"} {
		if err := ValidatePort(port); err != nil {
			t.Errorf("ValidatePort(%q) = %v, want nil", port, err)
		}
	}
	for _, port := range []string{"", "0", "65536", "-1", "http", "80a", " 8090"} {
		if err := ValidatePort(port); err == nil {
			t.Errorf("ValidatePort(%q) = nil, want an error", port)
		}
	}
}

func TestLastLine(t *testing.T) {
	for _, tc := range []struct{ logs, want string }{
		{"", "no output"},
		{"\n\n", "no output"},
		{"one\ntwo\n", "two"},
		{"one\r\nbind: address already in use\r\n", "bind: address already in use"},
	} {
		if got := lastLine(tc.logs); got != tc.want {
			t.Errorf("lastLine(%q) = %q, want %q", tc.logs, got, tc.want)
		}
	}
}
