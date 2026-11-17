package netbootmgr

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWithManagerRoundTrips(t *testing.T) {
	m := NewManager("", nil)
	ctx := WithManager(context.Background(), m)
	if got := FromContext(ctx); got != m {
		t.Errorf("FromContext = %v, want the Manager passed to WithManager", got)
	}
}

func TestFromContextWithNoManagerSetReturnsNil(t *testing.T) {
	if got := FromContext(context.Background()); got != nil {
		t.Errorf("FromContext on a plain context = %v, want nil", got)
	}
}

type fakeSink struct {
	chunks []string
}

func (f *fakeSink) BroadcastNetbootLogChunk(chunk string) {
	f.chunks = append(f.chunks, chunk)
}

func TestGetLogsEmptyBeforeStart(t *testing.T) {
	m := NewManager("", nil)
	if got := m.GetLogs(); got != "" {
		t.Errorf("GetLogs() before any Start = %q, want empty", got)
	}
}

func TestLogWriterAppendsBroadcastsAndPassesThrough(t *testing.T) {
	sink := &fakeSink{}
	m := NewManager("", sink)
	var passOn strings.Builder
	w := &logWriter{m: m, passOn: &passOn}

	n, err := w.Write([]byte("dhcp: request from 00:11:22:33:44:55\n"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len("dhcp: request from 00:11:22:33:44:55\n") {
		t.Errorf("Write returned n=%d, want full length", n)
	}

	if got := m.GetLogs(); got != "dhcp: request from 00:11:22:33:44:55\n" {
		t.Errorf("GetLogs() = %q, want the written chunk", got)
	}
	if passOn.String() != "dhcp: request from 00:11:22:33:44:55\n" {
		t.Errorf("passOn writer = %q, want the written chunk", passOn.String())
	}
	if len(sink.chunks) != 1 || sink.chunks[0] != "dhcp: request from 00:11:22:33:44:55\n" {
		t.Errorf("sink.chunks = %v, want a single matching chunk", sink.chunks)
	}
}

func TestLogWriterNilSinkDoesNotPanic(t *testing.T) {
	m := NewManager("", nil)
	w := &logWriter{m: m, passOn: &strings.Builder{}}
	if _, err := w.Write([]byte("no sink wired\n")); err != nil {
		t.Fatalf("Write returned error with a nil sink: %v", err)
	}
	if got := m.GetLogs(); got != "no sink wired\n" {
		t.Errorf("GetLogs() = %q, want the written chunk even with no sink", got)
	}
}

func TestLogWriterBoundsBufferToMaxLogBytes(t *testing.T) {
	m := NewManager("", nil)
	w := &logWriter{m: m, passOn: &strings.Builder{}}

	// Write well past the cap in two chunks, so the bound has to trim
	// mid-buffer, not just refuse a single too-large write.
	first := strings.Repeat("a", maxLogBytes-10)
	second := strings.Repeat("b", 100)
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if _, err := w.Write([]byte(second)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got := m.GetLogs()
	if len(got) != maxLogBytes {
		t.Fatalf("GetLogs() length = %d, want exactly maxLogBytes (%d)", len(got), maxLogBytes)
	}
	if !strings.HasSuffix(got, second) {
		t.Errorf("GetLogs() should retain the most recent write in full")
	}
	if strings.Contains(got, "a") == false {
		t.Errorf("GetLogs() should still retain some of the older content, not just the newest write")
	}
}

func TestHostFromURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"full url with port", "http://fleet.home.arpa:8090", "fleet.home.arpa"},
		{"full url without port", "https://fleet.home.arpa", "fleet.home.arpa"},
		{"url with path", "http://fleet.home.arpa:8080/ui", "fleet.home.arpa"},
		{"ipv4 url", "http://10.0.0.5:8080", "10.0.0.5"},
		{"ipv6 url strips brackets", "http://[fd00::1]:8080", "fd00::1"},
		// web.go documents --url as a URL, but an operator writing a bare host
		// should still get their host back rather than the wildcard.
		{"bare host and port", "fleet.home.arpa:8090", "fleet.home.arpa"},
		{"bare host", "fleet.home.arpa", "fleet.home.arpa"},
		{"scheme only", "http://", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostFromURL(tc.in); got != tc.want {
				t.Errorf("hostFromURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAdvertisedHostPrefersConfiguredURL(t *testing.T) {
	if got := advertisedHost("http://fleet.home.arpa:8080"); got != "fleet.home.arpa" {
		t.Errorf("advertisedHost = %q, want fleet.home.arpa", got)
	}
}

func TestAdvertisedHostFallsBackToALocalAddress(t *testing.T) {
	got := advertisedHost("")

	// A host with no up, non-loopback interface can only report the wildcard;
	// anywhere else the point of the fallback is that it does not.
	if localIPv4() == "" {
		if got != bindAddress {
			t.Errorf("advertisedHost = %q with no local address, want %q", got, bindAddress)
		}
		return
	}

	if got == bindAddress {
		t.Fatalf("advertisedHost = %q, want a routable local address", got)
	}
	ip := net.ParseIP(got)
	if ip == nil || ip.To4() == nil {
		t.Fatalf("advertisedHost = %q, want an IPv4 address", got)
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		t.Errorf("advertisedHost = %q, want an address clients can reach", got)
	}
}

// fakeNetbootArtifacts lays out the three files Start insists on and returns the
// artifacts dir and artifact ID to pass it.
func fakeNetbootArtifacts(t *testing.T) (string, string) {
	t.Helper()
	artifactsDir := t.TempDir()
	artifactID := "a5ec6329-e099-4720-97cf-aef72fc961f7"
	netbootDir := filepath.Join(artifactsDir, artifactID, "netboot")
	if err := os.MkdirAll(netbootDir, 0755); err != nil {
		t.Fatalf("mkdir netboot dir: %v", err)
	}
	for _, name := range []string{"kairos-kernel", "kairos-initrd", "kairos.squashfs"} {
		if err := os.WriteFile(filepath.Join(netbootDir, name), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return artifactsDir, artifactID
}

// stubAuroraboot puts an "auroraboot" that just blocks first on PATH, so Start
// gets a live child process without a real pixiecore.
func stubAuroraboot(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "auroraboot")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nsleep 60\n"), 0755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestStartReportsTheAdvertisedHostAndStillBindsTheWildcard(t *testing.T) {
	stubAuroraboot(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)

	m := NewManager("http://fleet.home.arpa:8080", nil)
	if err := m.Start(artifactsDir, artifactID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	s := m.GetStatus()
	if !s.Running {
		t.Fatal("Running = false after a successful Start")
	}
	if s.Address != bindAddress {
		t.Errorf("Address = %q, want the wildcard bind address %q", s.Address, bindAddress)
	}
	if s.AdvertisedAddress != "fleet.home.arpa" {
		t.Errorf("AdvertisedAddress = %q, want fleet.home.arpa", s.AdvertisedAddress)
	}
	// The netboot HTTP port is the manager's own, not the one in --url.
	if s.Port != "8090" {
		t.Errorf("Port = %q, want 8090", s.Port)
	}
}

func TestStartStillBindsTheWildcardWhenAdvertisingALocalAddress(t *testing.T) {
	stubAuroraboot(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)

	m := NewManager("", nil)
	if err := m.Start(artifactsDir, artifactID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	s := m.GetStatus()
	if s.Address != bindAddress {
		t.Errorf("Address = %q, want the wildcard bind address %q", s.Address, bindAddress)
	}
	if s.AdvertisedAddress != advertisedHost("") {
		t.Errorf("AdvertisedAddress = %q, want %q", s.AdvertisedAddress, advertisedHost(""))
	}
}

// TestStartWithPathsSharesStateWithStart exists because StepStartNetboot
// (deployer/steps.go) has its own already-resolved file paths and calls this
// method directly, bypassing the <artifactsDir>/<artifactID>/netboot/
// convention Start assumes. The point of StartWithPaths existing at all is
// that this routes through the same Status/mutex Start does, so a build's
// auto-started netboot server is visible to, and stoppable from, the same
// dashboard endpoints a manually-started one is.
func TestStartWithPathsSharesStateWithStart(t *testing.T) {
	stubAuroraboot(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)
	netbootDir := filepath.Join(artifactsDir, artifactID, "netboot")

	m := NewManager("", nil)
	err := m.StartWithPaths(
		artifactID,
		"", // no cloud-config attached
		filepath.Join(netbootDir, "kairos.squashfs"),
		filepath.Join(netbootDir, "kairos-initrd"),
		filepath.Join(netbootDir, "kairos-kernel"),
		"", // no livecd grub config
	)
	if err != nil {
		t.Fatalf("StartWithPaths: %v", err)
	}
	defer func() { _ = m.Stop() }()

	s := m.GetStatus()
	if !s.Running {
		t.Fatal("Running = false after a successful StartWithPaths")
	}
	if s.ArtifactID != artifactID {
		t.Errorf("ArtifactID = %q, want %q", s.ArtifactID, artifactID)
	}

	// Same guard Start has: a second start while one is already running is
	// rejected, not queued or silently collided with -- this is the bug that
	// prompted StartWithPaths to exist, reproduced directly.
	if err := m.StartWithPaths(artifactID, "", "x", "y", "z", ""); !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("StartWithPaths while already running: got %v, want ErrAlreadyRunning", err)
	}
}

// TestStartPixieArgsPutsGrubCfgBeforeThePositionals is the QA finding on
// kairos-io/kairos#2573: urfave/cli v2 stops parsing flags at the first
// positional argument, so a --grub-cfg appended after them is accepted and
// ignored. The netboot cmdline then silently loses the ISO's options.
func TestStartPixieArgsPutsGrubCfgBeforeThePositionals(t *testing.T) {
	args := startPixieArgs("config.yaml", "kairos.squashfs", "0.0.0.0", "8090", "kairos-initrd", "kairos-kernel", "kairos-grub.cfg")

	want := []string{
		"start-pixie",
		"--grub-cfg", "kairos-grub.cfg",
		"config.yaml", "kairos.squashfs", "0.0.0.0", "8090", "kairos-initrd", "kairos-kernel",
	}
	if len(args) != len(want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %q, want %q", args, want)
		}
	}
}

func TestStartPixieArgsOmitsGrubCfgWhenThereIsNone(t *testing.T) {
	args := startPixieArgs("", "kairos.squashfs", "0.0.0.0", "8090", "kairos-initrd", "kairos-kernel", "")

	for _, a := range args {
		if a == "--grub-cfg" {
			t.Fatalf("args = %q, want no --grub-cfg when the path is empty", args)
		}
	}
	// The empty cloud-config still has to hold its position, or squashfs
	// lands in the cloud-config slot.
	if args[1] != "" || args[2] != "kairos.squashfs" {
		t.Errorf("args = %q, want an empty cloud-config in position 1", args)
	}
}

// TestStartPassesTheExtractedGrubCfg covers the web UI path of the #2573 QA
// failure: the build writes netboot/kairos-grub.cfg, but the netboot server
// was started without --grub-cfg, so a dashboard netboot still booted the
// hardcoded cmdline.
func TestStartPassesTheExtractedGrubCfg(t *testing.T) {
	recorded := stubAurorabootRecordingArgs(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)
	grubCfg := filepath.Join(artifactsDir, artifactID, "netboot", "kairos-grub.cfg")
	if err := os.WriteFile(grubCfg, []byte("menuentry {}"), 0644); err != nil {
		t.Fatalf("write grub cfg: %v", err)
	}

	m := NewManager("", nil)
	if err := m.Start(artifactsDir, artifactID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	got := readRecordedArgs(t, recorded)
	if !strings.Contains(got, "--grub-cfg "+grubCfg) {
		t.Errorf("start-pixie args = %q, want --grub-cfg %s", got, grubCfg)
	}
}

func TestStartOmitsGrubCfgWhenTheIsoHadNone(t *testing.T) {
	recorded := stubAurorabootRecordingArgs(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)

	m := NewManager("", nil)
	if err := m.Start(artifactsDir, artifactID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = m.Stop() }()

	got := readRecordedArgs(t, recorded)
	if strings.Contains(got, "--grub-cfg") {
		t.Errorf("start-pixie args = %q, want no --grub-cfg when the ISO had no grub config", got)
	}
}

// TestStartWithPathsDropsAGrubCfgThatIsNotThere keeps the deployer honest:
// StepStartNetboot hands over netbootGrubCfgFile() unconditionally, and
// ExtractNetboot deletes that file when the ISO has no grub config.
func TestStartWithPathsDropsAGrubCfgThatIsNotThere(t *testing.T) {
	recorded := stubAurorabootRecordingArgs(t)
	artifactsDir, artifactID := fakeNetbootArtifacts(t)
	netbootDir := filepath.Join(artifactsDir, artifactID, "netboot")

	m := NewManager("", nil)
	err := m.StartWithPaths(
		artifactID,
		"",
		filepath.Join(netbootDir, "kairos.squashfs"),
		filepath.Join(netbootDir, "kairos-initrd"),
		filepath.Join(netbootDir, "kairos-kernel"),
		filepath.Join(netbootDir, "kairos-grub.cfg"),
	)
	if err != nil {
		t.Fatalf("StartWithPaths: %v", err)
	}
	defer func() { _ = m.Stop() }()

	got := readRecordedArgs(t, recorded)
	if strings.Contains(got, "--grub-cfg") {
		t.Errorf("start-pixie args = %q, want no --grub-cfg for a path that does not exist", got)
	}
}

// stubAurorabootRecordingArgs is stubAuroraboot plus a record of the argv it
// was called with, so a test can assert what reached the subprocess rather
// than only what the arg builder returned.
func stubAurorabootRecordingArgs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	recorded := filepath.Join(dir, "args")
	stub := filepath.Join(dir, "auroraboot")
	script := "#!/bin/sh\nprintf '%s ' \"$@\" > " + recorded + "\nsleep 60\n"
	if err := os.WriteFile(stub, []byte(script), 0755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return recorded
}

// readRecordedArgs waits for the stub to have written its argv. The Manager
// returns as soon as cmd.Start succeeds, so the child may not have run yet.
func readRecordedArgs(t *testing.T, path string) string {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stub never recorded its arguments at %s", path)
	return ""
}
