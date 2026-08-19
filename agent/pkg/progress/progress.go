// Package progress emits stable, machine-readable installation progress
// events on stdout for external installer frontends to consume.
//
// Events are emitted as JSON Lines (one JSON object per line) and only when
// the KAIROS_AGENT_PROGRESS environment variable is set to a non-empty value,
// so that human-driven installs and the in-process interactive TUI are
// unaffected.
//
// The contract vocabulary (env var, event values, step names) lives in
// kairos-sdk/agentrun so the agent (emitter) and installer frontends
// (consumers) share one definition.
package progress

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/kairos-io/kairos-sdk/agentrun"
)

// stepEvent is the JSON shape of a step line: {"event":"step","step":"..."}.
type stepEvent struct {
	Event string `json:"event"`
	Step  string `json:"step"`
}

// errorEvent is the JSON shape of an error line: {"event":"error","message":"..."}.
type errorEvent struct {
	Event   string `json:"event"`
	Message string `json:"message"`
}

// Output is where events are written. Overridable in tests.
var Output io.Writer = os.Stdout

func enabled() bool { return os.Getenv(agentrun.EnvProgress) != "" }

// emit marshals v as a single JSON line if emission is enabled.
func emit(v any) {
	if !enabled() {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(Output, "%s\n", b)
}

// EmitStep writes a step event if emission is enabled.
func EmitStep(step string) {
	emit(stepEvent{Event: agentrun.EventStep, Step: step})
}

// EmitError writes a failure event if emission is enabled.
func EmitError(msg string) {
	emit(errorEvent{Event: agentrun.EventError, Message: msg})
}
