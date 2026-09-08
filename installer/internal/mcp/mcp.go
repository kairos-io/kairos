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
// stdio only. The machine an installer runs on is live-booted and
// unauthenticated, so listening on the network would let anything that can
// reach it repartition the disk. Driving the install therefore requires the
// ability to spawn a process on the machine already, which is the same bar as
// running the TUI.
//
// # The install tool is destructive
//
// install repartitions a disk. It refuses to run unless the caller passes
// confirm=true and names a device that is currently an installation candidate,
// so an agent cannot wipe a disk through a hallucinated device path or by
// calling the tool with defaults.
package mcp

import (
	"context"
	"fmt"
	"io"
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

	// installing guards against two concurrent installs on one session.
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

// Serve runs the server on stdio until the client disconnects or ctx is done.
//
// Only a failure to start is an error. Every session ends with the client
// closing the stream, which the transport reports as an error of its own, so
// that one is logged rather than returned: a caller cannot tell "the agent is
// done" from "the installer is broken" if both come back the same way.
func Serve(ctx context.Context, log sdkLogger.KairosLogger) error {
	session, err := New(log).MCPServer().Connect(ctx, &mcp.StdioTransport{}, nil)
	if err != nil {
		return err
	}

	if err := session.Wait(); err != nil {
		log.Logger.Info().Err(err).Msg("MCP session ended")
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
