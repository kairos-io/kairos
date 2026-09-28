package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/prereqs"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
)

// Tool names, so tests and callers do not spell them out.
const (
	ToolListDisks          = "list_disks"
	ToolListPrerequisites  = "list_prerequisites"
	ToolApplyPrerequisites = "apply_prerequisites"
	ToolInstallOptions     = "get_install_options"
	ToolInstall            = "install"
	ToolDebugBundle        = "collect_debug_bundle"
)

// --- tool inputs and outputs ---

type listDisksInput struct{}

type listDisksOutput struct {
	Disks []disks.Disk `json:"disks" jsonschema:"the disks an installation can target"`
}

type listPrerequisitesInput struct {
	Config string `json:"config,omitempty" jsonschema:"the cloud-config the checks should be run against, if any"`
}

type listPrerequisitesOutput struct {
	Checks []prereqs.Check `json:"checks" jsonschema:"the checks the installed prerequisite plugins reported"`
}

type applyPrerequisitesInput struct {
	Decisions []prereqs.Decision `json:"decisions" jsonschema:"one decision per check to act on, keyed by the check's id"`
	Config    string             `json:"config,omitempty" jsonschema:"the cloud-config the decisions apply to, if any"`
}

type applyPrerequisitesOutput struct {
	Results []prereqs.ApplyResult `json:"results" jsonschema:"the outcome the plugins reported for each decision"`
}

type installOptionsInput struct{}

type installOptionsOutput struct {
	AgentBinary     string       `json:"agent_binary" jsonschema:"path to the kairos-agent binary that performs the install, empty when none was found"`
	FinishActions   []string     `json:"finish_actions" jsonschema:"accepted values of the install tool's finish_action"`
	Steps           []string     `json:"steps" jsonschema:"the progress steps an install reports, in the order the agent emits them"`
	Disks           []disks.Disk `json:"disks" jsonschema:"the disks an installation can target"`
	MinDiskBytes    uint64       `json:"min_disk_bytes" jsonschema:"the smallest disk offered as an installation target"`
	RequiresConfirm bool         `json:"requires_confirm" jsonschema:"whether the install tool requires confirm=true, which it always does"`
}

type installInput struct {
	Device       string `json:"device" jsonschema:"the disk to install to. Must be one of the paths list_disks returns. Everything on it is destroyed"`
	Confirm      bool   `json:"confirm" jsonschema:"must be true. Set it only after the person asking for the install has agreed to lose everything on device"`
	Source       string `json:"source,omitempty" jsonschema:"the image to install from, passed through to kairos-agent. Defaults to the source the installer was started with"`
	FinishAction string `json:"finish_action,omitempty" jsonschema:"what to do when the install finishes: reboot, poweroff or none. Defaults to none"`
	CloudConfig  string `json:"cloud_config,omitempty" jsonschema:"extra cloud-config YAML to merge into the install, for users, SSH keys or yip stages. The device, source and finish action above always win"`
}

type installOutput struct {
	Device       string   `json:"device" jsonschema:"the disk that was installed to"`
	StepsReached []string `json:"steps_reached" jsonschema:"the progress steps the agent reported, in order"`
	CloudConfig  string   `json:"cloud_config" jsonschema:"the cloud-config the install actually ran with"`
	Succeeded    bool     `json:"succeeded" jsonschema:"whether the install finished without an error"`
	Error        string   `json:"error,omitempty" jsonschema:"what went wrong, when it did"`
}

type debugBundleInput struct{}

type debugBundleOutput struct {
	Path string `json:"path" jsonschema:"path to the debug bundle that was written"`
}

// MCPServer builds the server with every tool registered.
func (s *Server) MCPServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: ServerVersion}, nil)

	readOnly := &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: ptr(false),
		OpenWorldHint:   ptr(false),
	}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolListDisks,
		Description: "List the disks this machine can be installed to, with their sizes. Nothing is written.",
		Annotations: readOnly,
	}, s.listDisks)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolListPrerequisites,
		Description: "Run the prerequisite checks the installed provider plugins report, the same ones the " +
			"interactive installer shows before an install. A check with severity \"error\" and blocking " +
			"true has to be resolved before the install can proceed.",
		Annotations: readOnly,
	}, s.listPrerequisites)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolApplyPrerequisites,
		Description: "Act on the prerequisite checks by sending one decision per check id. What a decision does " +
			"is the plugin's business and can change the machine, for example by wiping an LVM off a disk, so " +
			"read the check's message before answering it.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)},
	}, s.applyPrerequisites)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolInstallOptions,
		Description: "Describe what an install needs: the agent binary that will run it, the disks it can " +
			"target, the accepted finish actions, and the progress steps it reports. Nothing is written.",
		Annotations: readOnly,
	}, s.installOptions)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolInstall,
		Description: "Install Kairos to a disk. THIS DESTROYS EVERYTHING ON THAT DISK. It repartitions the " +
			"device, so only call it with confirm=true after the person asking for the install has agreed to " +
			"lose the disk's contents, and with a device taken from list_disks rather than guessed.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true), OpenWorldHint: ptr(false)},
	}, s.install)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolDebugBundle,
		Description: "Collect a debug bundle from this machine and return its path. The bundle may contain " +
			"sensitive data, so review it before sending it anywhere.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(false), OpenWorldHint: ptr(false)},
	}, s.collectDebugBundle)

	return srv
}

// --- tool handlers ---

func (s *Server) listDisks(_ context.Context, _ *mcp.CallToolRequest, _ listDisksInput) (*mcp.CallToolResult, listDisksOutput, error) {
	found, err := s.scanDisks()
	if err != nil {
		return errorResult("could not list disks: %v", err), listDisksOutput{}, nil
	}

	if len(found) == 0 {
		return textResult("No disk on this machine can be installed to. Disks under %s, and loop, ram, sr and zram devices, are not candidates.",
			disks.HumanSize(disks.MinSizeBytes)), listDisksOutput{Disks: []disks.Disk{}}, nil
	}

	lines := make([]string, 0, len(found))
	for _, d := range found {
		line := fmt.Sprintf("%s  %s", d.Path, d.Size)
		if d.Model != "" {
			line += "  " + d.Model
		}
		lines = append(lines, line)
	}

	return textResult("%s", strings.Join(lines, "\n")), listDisksOutput{Disks: found}, nil
}

func (s *Server) listPrerequisites(_ context.Context, _ *mcp.CallToolRequest, in listPrerequisitesInput) (*mcp.CallToolResult, listPrerequisitesOutput, error) {
	found, err := s.gatherChecks(in.Config)
	// Gather returns what it collected alongside a parse error from one
	// plugin, so report both rather than throwing the good checks away.
	if err != nil {
		s.log.Logger.Warn().Err(err).Msg("A prerequisite plugin returned something unparseable")
	}

	if len(found) == 0 {
		msg := "No prerequisite checks were reported. Either no check plugins are installed, or they found nothing to report."
		if err != nil {
			msg = fmt.Sprintf("%s A plugin also failed: %v", msg, err)
		}
		return textResult("%s", msg), listPrerequisitesOutput{Checks: []prereqs.Check{}}, nil
	}

	lines := make([]string, 0, len(found))
	for _, c := range found {
		line := fmt.Sprintf("[%s] %s: %s", c.Severity, c.ID, c.Title)
		if c.Blocking {
			line += " (blocking)"
		}
		lines = append(lines, line)
	}
	if err != nil {
		lines = append(lines, fmt.Sprintf("A plugin also failed: %v", err))
	}

	return textResult("%s", strings.Join(lines, "\n")), listPrerequisitesOutput{Checks: found}, nil
}

func (s *Server) applyPrerequisites(_ context.Context, _ *mcp.CallToolRequest, in applyPrerequisitesInput) (*mcp.CallToolResult, applyPrerequisitesOutput, error) {
	if len(in.Decisions) == 0 {
		return errorResult("no decisions were given, so there is nothing to apply"), applyPrerequisitesOutput{}, nil
	}

	results, err := s.applyDecisions(in.Decisions, in.Config)
	if err != nil {
		s.log.Logger.Warn().Err(err).Msg("A prerequisite plugin failed to apply")
	}

	lines := make([]string, 0, len(results))
	for _, r := range results {
		outcome := "ok"
		if !r.Success {
			outcome = "failed"
		}
		line := fmt.Sprintf("%s: %s", r.ID, outcome)
		if r.Message != "" {
			line += " - " + r.Message
		}
		lines = append(lines, line)
	}
	if err != nil {
		lines = append(lines, fmt.Sprintf("A plugin also failed: %v", err))
	}
	if len(lines) == 0 {
		lines = append(lines, "No plugin claimed any of these decisions.")
	}

	return textResult("%s", strings.Join(lines, "\n")), applyPrerequisitesOutput{Results: results}, nil
}

func (s *Server) installOptions(_ context.Context, _ *mcp.CallToolRequest, _ installOptionsInput) (*mcp.CallToolResult, installOptionsOutput, error) {
	out := installOptionsOutput{
		AgentBinary:     s.installer.ResolveAgentBin(),
		FinishActions:   FinishActions,
		Steps:           agentrun.Steps,
		MinDiskBytes:    disks.MinSizeBytes,
		RequiresConfirm: true,
	}

	found, err := s.scanDisks()
	if err != nil {
		return errorResult("could not list disks: %v", err), out, nil
	}
	out.Disks = found

	if out.AgentBinary == "" {
		return errorResult("no kairos-agent binary was found, so nothing can be installed from this machine"), out, nil
	}

	return textResult("kairos-agent at %s can install to one of %d disks. install requires confirm=true.",
		out.AgentBinary, len(found)), out, nil
}

func (s *Server) install(ctx context.Context, req *mcp.CallToolRequest, in installInput) (*mcp.CallToolResult, installOutput, error) {
	out := installOutput{Device: in.Device}

	if !in.Confirm {
		return errorResult("refusing to install: confirm was not true. %s destroys everything on %q, so ask whoever wants the install to agree to that first, then call again with confirm=true.",
			ToolInstall, in.Device), out, nil
	}
	if in.Device == "" {
		return errorResult("refusing to install: no device was given. Call %s and pass one of the paths it returns.", ToolListDisks), out, nil
	}

	// A device the machine does not have is either a hallucination or a stale
	// answer from an earlier scan, and either way must not reach the agent.
	known, names, err := s.knownDevices()
	if err != nil {
		return errorResult("could not list disks to check %q against: %v", in.Device, err), out, nil
	}
	if !known[in.Device] {
		return errorResult("refusing to install: %q is not an installation candidate on this machine. The candidates are: %s.", in.Device, names), out, nil
	}

	finishAction := in.FinishAction
	if finishAction == "" {
		finishAction = FinishNone
	}
	if !validFinishAction(finishAction) {
		return errorResult("finish_action %q is not one of %s", in.FinishAction, strings.Join(FinishActions, ", ")), out, nil
	}

	agentBin := s.installer.ResolveAgentBin()
	if agentBin == "" {
		return errorResult("no kairos-agent binary was found, so nothing can be installed"), out, nil
	}

	// One install per server. A second one would race the first over the same
	// disk, and a retry after a successful install would wipe what was just
	// written.
	if !s.installing.TryLock() {
		return errorResult("an install is already running on this session"), out, nil
	}
	defer s.installing.Unlock()
	if s.installed {
		return errorResult("this session already installed to %q. Start a new installer to install again.", in.Device), out, nil
	}

	cloudConfig, err := renderCloudConfig(in.Device, in.Source, finishAction, in.CloudConfig)
	if err != nil {
		return errorResult("could not build the cloud-config: %v", err), out, nil
	}
	out.CloudConfig = cloudConfig

	cfgPath, cleanup, err := s.writeCloudConfig(cloudConfig)
	if err != nil {
		return errorResult("could not write the cloud-config: %v", err), out, nil
	}
	defer cleanup()

	s.log.Logger.Info().Str("device", in.Device).Str("agent", agentBin).Msg("Starting an install driven over MCP")

	var sawError string
	runErr := s.installer.Run(agentBin, cfgPath, in.Source, finishAction,
		func(ev agentrun.ProgressEvent) {
			switch ev.Event {
			case agentrun.EventStep:
				out.StepsReached = append(out.StepsReached, ev.Step)
				s.notifyProgress(ctx, req, ev.Step, len(out.StepsReached))
			case agentrun.EventError:
				if sawError == "" {
					sawError = ev.Message
				}
			}
		},
		func(line string) { s.log.Print(line) },
	)

	switch {
	case sawError != "":
		out.Error = sawError
	case runErr != nil:
		out.Error = runErr.Error()
	}

	if out.Error != "" {
		return errorResult("the install failed after reaching %s: %s", stepList(out.StepsReached), out.Error), out, nil
	}

	out.Succeeded = true
	s.installed = true

	return textResult("Installed Kairos to %s. Steps reached: %s.", in.Device, stepList(out.StepsReached)), out, nil
}

func (s *Server) collectDebugBundle(_ context.Context, _ *mcp.CallToolRequest, _ debugBundleInput) (*mcp.CallToolResult, debugBundleOutput, error) {
	path, err := s.generateBundle()
	if err != nil {
		return errorResult("could not collect a debug bundle: %v", err), debugBundleOutput{}, nil
	}

	return textResult("Wrote a debug bundle to %s. Review it before sending it anywhere, it may contain sensitive data.", path),
		debugBundleOutput{Path: path}, nil
}

// notifyProgress reports a step to a client that asked for progress. A client
// that did not, or one that has gone away, is not an install failure.
func (s *Server) notifyProgress(ctx context.Context, req *mcp.CallToolRequest, step string, done int) {
	if req == nil || req.Session == nil || req.Params == nil || req.Params.GetProgressToken() == nil {
		return
	}

	err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
		ProgressToken: req.Params.GetProgressToken(),
		Message:       step,
		Progress:      float64(done),
		Total:         float64(len(agentrun.Steps)),
	})
	if err != nil {
		s.log.Logger.Debug().Err(err).Msg("Could not report install progress to the client")
	}
}

func validFinishAction(a string) bool {
	for _, valid := range FinishActions {
		if a == valid {
			return true
		}
	}

	return false
}

func stepList(steps []string) string {
	if len(steps) == 0 {
		return "no steps"
	}

	return strings.Join(steps, ", ")
}
