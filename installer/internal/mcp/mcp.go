// Package mcp serves the Kairos installer over the Model Context Protocol, so
// an agent can drive an installation the way a person drives the TUI.
//
// It is a third frontend on an existing contract, not new install logic. The
// TUI, the web UI and this all reach kairos-agent through sdk/agentrun, which
// turns `kairos-agent manual-install` into a stream of typed progress events.
// Everything here does is expose the same information the TUI screens show and
// the same install call the TUI makes.
//
// # Transport
//
// Streamable HTTP, served by kairos-installer alongside the TUI, so an agent
// reaches the installer the same way a person reaches the web UI: over the
// network, without having to spawn a process on the machine first. That is the
// exposure the live image already has, because kairos-webui listens on :8080
// on the same boot and can install from there.
//
// [Handler] is the server as an http.Handler so it can be mounted on the
// installer's own mux once the web UI moves in (kairos-io/kairos#4340);
// [ListenAndServe] is the standalone listener used until then.
//
// # Who can reach it
//
// Nobody is authenticated. Anything that can reach the listening address can
// call every tool, install included. The cross-origin wrapper below is a
// browser control and nothing else: it decides on Sec-Fetch-Site and Origin,
// which a non-browser caller does not send, so it stops a page the operator
// opened and not a program on the network.
//
// That is deliberate, and it is the exposure a live-booted machine already has
// from kairos-webui on :8080, which can install too. An operator who does not
// want it turns the listener off or moves it to loopback through the agent
// config, the same two knobs the web UI takes:
//
//	mcp:
//	  disable: true
//	  listen_address: 127.0.0.1:8090
//
// # The install tool is destructive
//
// install repartitions a disk. It refuses to run unless the caller passes
// confirm=true and names a device that is currently an installation candidate,
// so an agent cannot wipe a disk through a hallucinated device path or by
// calling the tool with defaults. The guard is held by one [Server] shared by
// every HTTP session, so a client cannot get a second install by reconnecting.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kairos-io/kairos/v4/installer/internal/checks"
	"github.com/kairos-io/kairos/v4/installer/internal/debugbundle"
	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/prereqs"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
	"github.com/kairos-io/kairos/v4/sdk/branding"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// ServerName and ServerVersion identify this server to a client.
const (
	ServerName    = "kairos-installer"
	ServerVersion = "v1"
)

// Finish actions an install can end with. They are the same strings
// agentrun.Command understands.
const (
	FinishReboot   = "reboot"
	FinishPoweroff = "poweroff"
	FinishNone     = "none"
)

// FinishActions are the accepted values of the install tool's finish_action.
var FinishActions = []string{FinishReboot, FinishPoweroff, FinishNone}

// Installer is the part of the install path this server drives. It is an
// interface so the tools can be tested without repartitioning anything.
type Installer interface {
	// ResolveAgentBin returns the kairos-agent path, or "" when there is none.
	ResolveAgentBin() string
	// Run performs the install described by the cloud-config at cfgPath,
	// reporting each progress event to onEvent and each log line to onLog.
	Run(agentBin, cfgPath, source, finishAction string, onEvent func(agentrun.ProgressEvent), onLog func(string)) error
}

// agentInstaller is the real installer: kairos-agent through sdk/agentrun.
type agentInstaller struct{}

func (agentInstaller) ResolveAgentBin() string { return agentrun.ResolveAgentBin() }

func (agentInstaller) Run(agentBin, cfgPath, source, finishAction string, onEvent func(agentrun.ProgressEvent), onLog func(string)) error {
	// Capture the whole agent transcript into the same log the TUI writes, so
	// an install driven by an agent lands in a debug bundle like any other.
	var transcript io.Writer
	if lf, err := os.OpenFile(debugbundle.AgentOutputLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		defer lf.Close()
		transcript = lf
	}

	return agentrun.RunWithOutput(agentBin, cfgPath, source, finishAction, onEvent, onLog, transcript)
}

// Server holds what the tools need. Everything it reaches the outside world
// through is replaceable, so the tools are testable.
type Server struct {
	log sdkLogger.KairosLogger

	// scanDisks lists installation candidates.
	scanDisks func() ([]disks.Disk, error)
	// gatherChecks and applyDecisions run the prerequisite plugins.
	gatherChecks     func(config string) ([]prereqs.Check, error)
	applyDecisions   func(decisions []prereqs.Decision, config string) ([]prereqs.ApplyResult, error)
	installer        Installer
	generateBundle   func() (string, error)
	writeCloudConfig func(content string) (path string, cleanup func(), err error)

	// installing guards against two concurrent installs. It lives on the Server,
	// which every HTTP session shares, so reconnecting does not reset it.
	installing sync.Mutex
	installed  bool
}

// New builds a Server backed by the real host.
func New(log sdkLogger.KairosLogger) *Server {
	s := &Server{
		log:            log,
		scanDisks:      disks.Scan,
		installer:      agentInstaller{},
		generateBundle: func() (string, error) { return generateBundle(time.Now()) },
	}

	s.gatherChecks = func(config string) ([]prereqs.Check, error) {
		return checks.Gather(checks.NewManager(log), log, config)
	}
	s.applyDecisions = func(decisions []prereqs.Decision, config string) ([]prereqs.ApplyResult, error) {
		return checks.Apply(checks.NewManager(log), log, decisions, config)
	}
	s.writeCloudConfig = writeCloudConfig

	return s
}

// Defaults for the HTTP transport. The port sits next to the web UI's :8080 on
// the same live machine, and the path is the one MCP clients assume.
const (
	DefaultListenAddress = ":8090"
	Path                 = "/mcp"
)

// ListenAddressFromConfig resolves where to listen from /etc/kairos/agent.yaml,
// the same file and the same two knobs kairos-webui reads for itself. An empty
// result means do not listen at all.
//
// This is the only control an operator has on a real boot: kairos-agent execs
// the installer with a fixed argument list, so a flag never reaches it there.
// The variadic paths are for tests; main calls this with none and gets
// /etc/kairos/agent.yaml, the same file kairos-webui reads.
func ListenAddressFromConfig(paths ...string) string {
	cfg, err := branding.LoadConfig(paths...)
	if err != nil || cfg == nil {
		return DefaultListenAddress
	}

	return listenAddressFor(cfg.MCP)
}

func listenAddressFor(m branding.MCP) string {
	switch {
	case m.Disable:
		return ""
	case m.HasAddress():
		return m.ListenAddress
	default:
		return DefaultListenAddress
	}
}

// Handler returns the MCP server as an http.Handler, so it can be mounted on
// the installer's own mux next to the web UI.
//
// One Server backs every session. The "one install per boot" guard is held on
// it, so reconnecting does not hand a client a second install.
func Handler(log sdkLogger.KairosLogger) http.Handler {
	return handlerFor(New(log))
}

func handlerFor(s *Server) http.Handler {
	srv := s.MCPServer()

	h := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		// The zerolog logger is an io.Writer, so the SDK's transport logging
		// lands in the installer log rather than on the TUI's terminal.
		&mcp.StreamableHTTPOptions{Logger: slog.New(slog.NewTextHandler(s.log.Logger, nil))},
	)

	// The installer listens on a machine whose browser a person is also using,
	// so a page they visit must not be able to POST an install to it. This is
	// the only thing it does: a caller that sends no Origin and no
	// Sec-Fetch-Site is not a browser and passes straight through, which is
	// what the "Who can reach it" note above is about.
	return http.NewCrossOriginProtection().Handler(h)
}

// ListenAndServe serves the installer over MCP on addr until ctx is done.
//
// A client hanging up is not an error: the installer keeps listening for the
// next one. Only failing to bind, or the listener itself dying, comes back.
func ListenAndServe(ctx context.Context, log sdkLogger.KairosLogger, addr string) error {
	mux := http.NewServeMux()
	mux.Handle(Path, Handler(log))

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// Deliberately no read or write timeout: install streams progress
		// events for as long as the install takes, and either one would cut
		// the stream off part-way through writing a disk.
		ErrorLog: stdlog.New(log.Logger, "mcp: ", 0),
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	log.Logger.Info().Str("address", ln.Addr().String()).Str("path", Path).Msg("MCP server listening")

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-ctx.Done():
		case <-done:
			return
		}

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

// generateBundle collects a debug bundle the way the non-interactive
// --collect-debug-bundle flag does.
func generateBundle(now time.Time) (string, error) {
	agentBin := agentrun.ResolveAgentBin()
	ctx := debugbundle.Context{AgentBin: agentBin}
	if agentBin != "" {
		ctx.AgentArgs = agentrun.Command(agentBin, "<config>", "", "").Args[1:]
	}

	return debugbundle.GenerateBundle(agentBin, ctx, now)
}

// writeCloudConfig writes the rendered cloud-config where kairos-agent can
// read it, and returns a function that removes it again.
func writeCloudConfig(content string) (string, func(), error) {
	f, err := os.CreateTemp("", "kairos-mcp-install-*.yaml")
	if err != nil {
		return "", nil, err
	}

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", nil, err
	}

	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func ptr[T any](v T) *T { return &v }

func textResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

func errorResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

// knownDevices returns the current installation candidates as a set, and the
// list as text for an error message.
func (s *Server) knownDevices() (map[string]bool, string, error) {
	found, err := s.scanDisks()
	if err != nil {
		return nil, "", err
	}

	set := make(map[string]bool, len(found))
	names := make([]string, 0, len(found))
	for _, d := range found {
		set[d.Path] = true
		names = append(names, d.Path)
	}

	if len(names) == 0 {
		return set, "none", nil
	}

	return set, strings.Join(names, ", "), nil
}
