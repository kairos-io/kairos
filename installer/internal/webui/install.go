package webui

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/installer/internal/debugbundle"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
)

// errNoAgent is returned when kairos-agent cannot be found. The message is
// shown to the operator, so it names the two ways to fix it.
var errNoAgent = errors.New("kairos-agent not found (set KAIROS_AGENT_BIN or add it to PATH)")

// startInstall writes the cloud-config to a temporary file and runs
// kairos-agent against it, publishing the agent's progress to log.
//
// source is the install source the installer itself was started with, and
// goes to the agent as --source. The agent merges it over the config file, so
// the source the boot asked for wins over one in the submitted cloud-config,
// and both frontends of one installer install the same image. Empty means the
// submitted cloud-config decides.
//
// It returns once the agent has been started, so the handler can redirect the
// browser to the progress page while the install runs. Errors it returns are
// the ones that happen before the agent is running, which are the only ones
// there is still a form to report them on; everything after that arrives as an
// error message on the progress stream.
func startInstall(log *progressLog, source, cloudConfig, device, finish string) error {
	// Resolve the agent before writing the config: it carries the operator's
	// password hash, so it must not be left on disk when nothing is going to
	// read it.
	agentBin := agentrun.ResolveAgentBin()
	if agentBin == "" {
		return errNoAgent
	}

	rendered, err := renderCloudConfig(cloudConfig, device)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "install-webui-*.yaml")
	if err != nil {
		return err
	}
	cfgPath := f.Name()
	_ = f.Close()
	if err := os.WriteFile(cfgPath, []byte(rendered), 0600); err != nil {
		_ = os.Remove(cfgPath)
		return err
	}

	go runAgent(log, agentBin, cfgPath, source, finish)
	return nil
}

// runAgent drives one agent run to completion and publishes what it says. It
// always publishes a done message, so a browser watching the stream is never
// left waiting on a run that has already ended.
func runAgent(log *progressLog, agentBin, cfgPath, source, finish string) {
	defer func() { _ = os.Remove(cfgPath) }()

	// sawError is only touched from the onEvent callback, which
	// agentrun.RunWithOutput calls synchronously on this goroutine.
	sawError := false

	// Tee the raw agent transcript into the same log the TUI writes, so an
	// install driven from the browser lands in a debug bundle too. It is
	// best-effort: a nil writer just means the bundle has no transcript.
	var transcript io.Writer
	if lf, err := openAgentTranscript(); err == nil {
		defer func() { _ = lf.Close() }()
		transcript = lf
	}

	err := agentrun.RunWithOutput(agentBin, cfgPath, source, finish,
		func(ev agentrun.ProgressEvent) {
			switch ev.Event {
			case agentrun.EventStep:
				log.publish(Message{Type: MessageStep, Step: ev.Step, Message: ev.Message})
			case agentrun.EventError:
				sawError = true
				log.publish(Message{Type: MessageError, Message: ev.Message})
			}
		},
		func(line string) {
			log.publish(Message{Type: MessageLog, Message: line})
		},
		transcript,
	)
	if err != nil && !sawError {
		log.publish(Message{Type: MessageError, Message: err.Error()})
	}
	log.publish(Message{Type: MessageDone, OK: err == nil && !sawError})
}

// openAgentTranscript opens the agent transcript log for appending, creating
// its directory. In --no-tui mode nothing else has created /var/log/kairos
// yet, because the installer logger that normally does never runs.
func openAgentTranscript() (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(debugbundle.AgentOutputLog), 0755); err != nil {
		return nil, err
	}
	return os.OpenFile(debugbundle.AgentOutputLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
