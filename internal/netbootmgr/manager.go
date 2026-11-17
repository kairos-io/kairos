package netbootmgr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// managerContextKey is unexported so only this package can set or read the
// value it names -- callers use WithManager/FromContext instead.
type managerContextKey struct{}

// WithManager returns a copy of ctx carrying m, retrievable with FromContext.
// The deployer's netboot step (deployer/steps.go's StepStartNetboot) uses
// this to route an artifact-build-triggered netboot start through the same
// Manager the dashboard's Start/Stop/Status endpoints use, instead of
// starting a server the UI has no way to see or stop -- see
// mission-control's incident notes on the two code paths not sharing state.
func WithManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, managerContextKey{}, m)
}

// FromContext returns the Manager stored by WithManager, or nil if none was
// set -- the normal case for the CLI, which has no dashboard to keep in sync.
func FromContext(ctx context.Context) *Manager {
	m, _ := ctx.Value(managerContextKey{}).(*Manager)
	return m
}

// bindAddress is the address the netboot server listens on. It is a wildcard
// because the server has to answer PXE clients on whichever interface they
// arrive from, which is also why it can never double as the address to hand
// out; see AdvertisedAddress.
const bindAddress = "0.0.0.0"

var (
	// ErrAlreadyRunning is returned when a start is asked for while a
	// netboot server is running.
	ErrAlreadyRunning = errors.New("netboot server is already running")
	// ErrNoNetbootFiles is returned by Start when the artifact lacks the
	// kernel, initrd or squashfs. The wrapping error names the missing file.
	ErrNoNetbootFiles = errors.New("artifact has no netboot files")
)

// Status represents the current state of the netboot server.
type Status struct {
	Running    bool   `json:"running"`
	ArtifactID string `json:"artifactId,omitempty"`
	// Address is the address the server binds to, always the wildcard.
	Address string `json:"address"`
	// AdvertisedAddress is the host to reach the server on: the host of the
	// configured external URL, or a local interface address when there is none.
	AdvertisedAddress string `json:"advertisedAddress"`
	Port              string `json:"port"`
}

// LogSink receives the netboot server's output as it happens, so a UI client
// watching a PXE boot in progress can see why it fails instead of only a
// stalled "Running" badge (kairos-io/kairos#4596). Satisfied by
// *ws.UIHub.BroadcastNetbootLogChunk; kept as a narrow interface here so this
// package does not import pkg/ws.
type LogSink interface {
	BroadcastNetbootLogChunk(chunk string)
}

// maxLogBytes bounds the retained log so a long-running or noisy PXE server
// (a node retrying DHCP forever) cannot grow this without limit; it is an
// in-memory debugging aid, not a durable log store.
const maxLogBytes = 256 * 1024

// Manager manages a PXE/netboot server lifecycle.
type Manager struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	status        Status
	address       string
	advertisedURL string
	port          string
	logSink       LogSink
	logBuf        bytes.Buffer
}

// NewManager creates a new netboot Manager. advertisedURL is the externally
// reachable URL of this AuroraBoot instance (--url / AURORABOOT_URL); only its
// host is used, since the netboot server has its own port. It may be empty.
// logSink may be nil (e.g. in tests), in which case output is still captured
// for GetLogs but never broadcast live.
func NewManager(advertisedURL string, logSink LogSink) *Manager {
	return &Manager{
		address:       bindAddress,
		advertisedURL: advertisedURL,
		port:          "8090",
		logSink:       logSink,
	}
}

// logWriter tees a running command's output to the process's own
// stdout/stderr (unchanged operational behaviour), into the Manager's bounded
// snapshot buffer, and to the live broadcaster. Each Write() call forwards
// its raw chunk as one broadcast; chunks are not split into lines, so a
// broadcast can carry a partial line.
type logWriter struct {
	m      *Manager
	passOn io.Writer
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.m.mu.Lock()
	if w.m.logBuf.Len()+len(p) > maxLogBytes {
		// Drop the oldest bytes rather than the newest: a debugging session
		// cares about what just happened, not the start of a long-idle server.
		overflow := w.m.logBuf.Len() + len(p) - maxLogBytes
		b := w.m.logBuf.Bytes()
		w.m.logBuf.Reset()
		if overflow < len(b) {
			w.m.logBuf.Write(b[overflow:])
		}
	}
	w.m.logBuf.Write(p)
	sink := w.m.logSink
	w.m.mu.Unlock()

	if sink != nil {
		sink.BroadcastNetbootLogChunk(string(p))
	}
	return w.passOn.Write(p)
}

// advertisedHost is the host clients should use to reach the netboot server.
// The configured external URL wins; failing that a local interface address is
// better than handing out the wildcard, which is reachable from nowhere.
func advertisedHost(advertisedURL string) string {
	if h := hostFromURL(advertisedURL); h != "" {
		return h
	}
	if ip := localIPv4(); ip != "" {
		return ip
	}
	return bindAddress
}

// hostFromURL extracts the host from a URL, tolerating a bare "host" or
// "host:port" with no scheme, which url.Parse reads as a scheme or a path.
func hostFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Once a scheme separator is present url.Parse is authoritative. Falling
	// through to SplitHostPort would read "http://" as host "http", port "//".
	if strings.Contains(raw, "//") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	if h, _, err := net.SplitHostPort(raw); err == nil && h != "" {
		return h
	}
	if !strings.ContainsAny(raw, "/:") {
		return raw
	}
	return ""
}

// localIPv4 returns an IPv4 address of the first up, non-loopback interface.
// Link-local addresses are skipped: they name an interface nobody can route to.
func localIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			return ip4.String()
		}
	}
	return ""
}

// Start launches the netboot server for the given artifact.
// It looks for kairos-kernel, kairos-initrd, and kairos.squashfs
// in <artifactsDir>/<artifactID>/netboot/.
func (m *Manager) Start(artifactsDir, artifactID string) error {
	netbootDir := filepath.Join(artifactsDir, artifactID, "netboot")

	kernel := filepath.Join(netbootDir, "kairos-kernel")
	initrd := filepath.Join(netbootDir, "kairos-initrd")
	squashfs := filepath.Join(netbootDir, "kairos.squashfs")

	// Verify required files exist.
	for _, f := range []string{kernel, initrd, squashfs} {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("%w: required netboot file not found: %s", ErrNoNetbootFiles, f)
		}
	}

	// If the artifact was built with a cloud-config attached, it's saved
	// alongside it as config.yaml. Serve it as config_url so kairos-agent's
	// sdk/collector (which reads config_url from /proc/cmdline) can fetch
	// it at boot -- the /oem datasource path only pulls from cloud-provider
	// metadata services, never from netboot's own HTTP delivery, so this is
	// the only way a netbooted bare-metal node gets configured at all.
	cloudConfig := ""
	if cfgPath := filepath.Join(artifactsDir, artifactID, "config.yaml"); fileExists(cfgPath) {
		cloudConfig = cfgPath
	}

	// The build leaves the livecd grub config next to the three artifacts
	// above, and the netboot cmdline is taken from it so a netbooted node
	// boots with the ISO's options. Optional, like the cloud-config: an ISO
	// without a grub config gets none extracted. See kairos-io/kairos#2573.
	grubCfg := filepath.Join(netbootDir, "kairos-grub.cfg")

	return m.StartWithPaths(artifactID, cloudConfig, squashfs, initrd, kernel, grubCfg)
}

// StartWithPaths launches the netboot server from already-resolved file
// paths, bypassing the <artifactsDir>/<artifactID>/netboot/ convention Start
// assumes. artifactID is used only for Status.ArtifactID; it need not be a
// real artifact store ID. Exported so a caller with its own paths in hand
// (deployer/steps.go's StepStartNetboot, via WithManager/FromContext) can
// still register with this Manager's shared state -- see WithManager's doc.
//
// grubCfg is the livecd grub config the netboot cmdline is taken from. It is
// optional, and a path that is not on disk is dropped rather than passed on:
// StepStartNetboot hands over its expected location unconditionally, and
// ExtractNetboot deletes that file when the ISO carries no grub config.
func (m *Manager) StartWithPaths(artifactID, cloudConfig, squashfs, initrd, kernel, grubCfg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status.Running {
		return ErrAlreadyRunning
	}

	if grubCfg != "" && !fileExists(grubCfg) {
		grubCfg = ""
	}

	cmd := exec.Command("auroraboot", startPixieArgs(cloudConfig, squashfs, m.address, m.port, initrd, kernel, grubCfg)...)
	// A fresh session starts with a clean log: the previous run's output (if
	// any) is no longer relevant to debugging this one.
	m.logBuf.Reset()
	cmd.Stdout = &logWriter{m: m, passOn: os.Stdout}
	cmd.Stderr = &logWriter{m: m, passOn: os.Stderr}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start netboot server: %w", err)
	}

	m.cmd = cmd
	m.status = Status{
		Running:    true,
		ArtifactID: artifactID,
		Address:    m.address,
		// Resolved per start, not once in NewManager, so an interface that came
		// up after the fleet server did is still picked up.
		AdvertisedAddress: advertisedHost(m.advertisedURL),
		Port:              m.port,
	}

	// Wait for the process in the background so we can detect if it exits.
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		// Only clear status if this is still the active command.
		if m.cmd == cmd {
			m.status.Running = false
			m.cmd = nil
		}
	}()

	return nil
}

// Stop kills the running netboot server subprocess.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.status.Running || m.cmd == nil {
		return fmt.Errorf("netboot server is not running")
	}

	if m.cmd.Process != nil {
		if err := m.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to stop netboot server: %w", err)
		}
	}

	m.cmd = nil
	m.status.Running = false
	m.status.ArtifactID = ""

	return nil
}

// GetStatus returns the current netboot server status.
func (m *Manager) GetStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// GetLogs returns a snapshot of the current (or most recently run) netboot
// session's captured stdout/stderr, bounded to maxLogBytes. Empty before the
// first Start call.
func (m *Manager) GetLogs() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.logBuf.String()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// startPixieArgs builds the argv for the `auroraboot start-pixie` subprocess:
//
//	start-pixie [--grub-cfg <path>] <cloud-config> <squashfs> <address> <port> <initrd> <kernel>
//
// cloud-config may be empty, and keeps its position when it is, so the
// squashfs does not slide into its slot.
//
// --grub-cfg goes before the positional arguments, not after. urfave/cli v2
// stops parsing flags at the first positional, so a flag appended at the end
// is accepted and silently ignored, and the netboot cmdline then loses the
// ISO's options without saying so. See kairos-io/kairos#2573.
func startPixieArgs(cloudConfig, squashfs, address, port, initrd, kernel, grubCfg string) []string {
	args := []string{"start-pixie"}
	if grubCfg != "" {
		args = append(args, "--grub-cfg", grubCfg)
	}

	return append(args, cloudConfig, squashfs, address, port, initrd, kernel)
}
