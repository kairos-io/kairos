// Package agentrun drives kairos-agent's manual-install and parses its
// JSON-Lines progress stream. It has no TUI dependencies so installer
// frontends (and tests) can reuse it in isolation.
//
// It implements the installer side of the kairos-agent installer contract:
// build a `kairos-agent manual-install` invocation, run it with progress
// emission enabled, and turn the agent's JSON-Lines stdout into structured
// progress events.
package agentrun

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/kairos-io/kairos-sdk/constants"
)

// EnvAgentBin overrides agent discovery with an explicit path.
const EnvAgentBin = constants.AgentEnvVar

// Contract vocabulary — the JSON-Lines progress protocol that kairos-agent
// emits and installer frontends consume. Both sides should reference these
// constants instead of hard-coding the strings.
const (
	// EnvProgress, when set to a non-empty value in the agent's environment,
	// makes kairos-agent emit progress events on stdout.
	EnvProgress = "KAIROS_AGENT_PROGRESS"

	// Event values for ProgressEvent.Event.
	EventStep  = "step"
	EventError = "error"
)

// Step values for ProgressEvent.Step, in the order the agent emits them.
const (
	StepPartition     = "partition"
	StepBeforeInstall = "before-install"
	StepActive        = "active"
	StepBootloader    = "bootloader"
	StepRecovery      = "recovery"
	StepPassive       = "passive"
	StepAfterInstall  = "after-install"
	StepDone          = "done"
)

// Steps lists the step events in the order the agent emits them on a full,
// successful install. Steps that do not run (e.g. partition on a NoFormat
// install) are simply omitted from the stream.
var Steps = []string{
	StepPartition,
	StepBeforeInstall,
	StepActive,
	StepBootloader,
	StepRecovery,
	StepPassive,
	StepAfterInstall,
	StepDone,
}

// ProgressEvent is one parsed JSON-Lines progress line from the agent.
type ProgressEvent struct {
	Event   string `json:"event"`
	Step    string `json:"step"`
	Message string `json:"message"`
}

// ResolveAgentBin returns the kairos-agent path. Resolution order matches the
// agent contract shared with kairos-init:
//
//	$KAIROS_AGENT_BIN (when set and existing) -> AgentDefaultPath -> kairos-agent on PATH.
func ResolveAgentBin() string {
	if p := os.Getenv(constants.AgentEnvVar); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat(constants.AgentDefaultPath); err == nil {
		return constants.AgentDefaultPath
	}
	if p, err := exec.LookPath(constants.AgentBinName); err == nil {
		return p
	}
	return ""
}

// Command builds the manual-install invocation. finishAction is one of
// "reboot", "poweroff", or anything else (no finish flag). It sets
// KAIROS_AGENT_PROGRESS=1 so the agent emits progress events.
func Command(agentBin, cfgPath, source, finishAction string) *exec.Cmd {
	args := []string{"manual-install"}
	if source != "" {
		args = append(args, "--source", source)
	}
	args = append(args, "--use-default-dirs")
	switch finishAction {
	case "reboot":
		args = append(args, "--reboot")
	case "poweroff":
		args = append(args, "--poweroff")
	}
	args = append(args, cfgPath)

	cmd := exec.Command(agentBin, args...)
	cmd.Env = append(os.Environ(), EnvProgress+"=1")
	return cmd
}

// ParseLine parses one stdout line. ok is true only for a JSON object carrying
// a non-empty "event" field; everything else (plain logs, eventless JSON) is
// reported as ok=false.
func ParseLine(line []byte) (ProgressEvent, bool) {
	var ev ProgressEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return ProgressEvent{}, false
	}
	if ev.Event == "" {
		return ProgressEvent{}, false
	}
	return ev, true
}

// Run execs the agent, calling onEvent for each progress event and onLog for
// each non-event stdout line. The agent's stderr is forwarded to os.Stderr.
// It returns the process exit error, if any.
func Run(agentBin, cfgPath, source, finishAction string, onEvent func(ProgressEvent), onLog func(string)) error {
	return run(agentBin, cfgPath, source, finishAction, onEvent, onLog, os.Stderr, nil)
}

// RunWithOutput behaves like Run but additionally tees the agent's complete
// output into out: every raw stdout line (progress JSON included), each
// followed by a newline, plus the agent's stderr stream. It is intended for
// capturing a full agent transcript in debug bundles. Writes from the stdout
// and stderr streams are serialized, so out need not be safe for concurrent
// use.
//
// If out is nil, RunWithOutput behaves like Run except the agent's stderr is
// discarded instead of forwarded to os.Stderr.
func RunWithOutput(agentBin, cfgPath, source, finishAction string, onEvent func(ProgressEvent), onLog func(string), out io.Writer) error {
	if out == nil {
		return run(agentBin, cfgPath, source, finishAction, onEvent, onLog, io.Discard, nil)
	}
	w := &lockedWriter{w: out}
	return run(agentBin, cfgPath, source, finishAction, onEvent, onLog, w, w)
}

// run is the shared implementation behind Run and RunWithOutput. stderr
// receives the agent's stderr stream; when stdoutTee is non-nil, each raw
// stdout line is written to it (with a trailing newline) before being parsed.
func run(agentBin, cfgPath, source, finishAction string, onEvent func(ProgressEvent), onLog func(string), stderr, stdoutTee io.Writer) error {
	cmd := Command(agentBin, cfgPath, source, finishAction)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if stdoutTee != nil {
			_, _ = stdoutTee.Write(line)
			_, _ = stdoutTee.Write([]byte{'\n'})
		}
		if ev, ok := ParseLine(line); ok {
			onEvent(ev)
		} else if len(line) > 0 {
			onLog(string(line))
		}
	}
	return cmd.Wait()
}

// lockedWriter serializes concurrent writes to an underlying writer, so the
// agent's stdout-tee (written from the scan loop) and stderr (written from the
// os/exec copy goroutine) can safely share a single destination.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
