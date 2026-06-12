// tui-check-magicword is a deliberately dumb example interactive-install check
// plugin (a "tui-check"). It exists to demonstrate the parts of the contract
// the diskwipe example does not:
//
//   - A free-text prompt that collects input from the user.
//   - A Required check whose apply step validates that input, so the install
//     cannot continue until the user gets it right. A wrong answer surfaces a
//     retry on the prerequisites screen.
//
// Behaviour: it asks the user to type a magic word. On apply it accepts only
// "jojo"; anything else fails and the user is asked to retry.
//
//	go build -o tui-check-magicword ./tui-check-examples/tui-check-magicword
//	install -m0755 tui-check-magicword /usr/local/system/tui-checks/
//
// Logs written to stderr are captured by go-pluggable into EventResponse.Logs
// and re-logged by the agent.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos-installer/prereqs"
	"github.com/mudler/go-pluggable"
)

const (
	checkID   = "magicword"
	magicWord = "jojo"
)

func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "tui-check-magicword: "+format+"\n", a...)
}

func main() {
	factory := pluggable.NewPluginFactory(
		pluggable.FactoryPlugin{EventType: prereqs.EventChecks, PluginHandler: onChecks},
		pluggable.FactoryPlugin{EventType: prereqs.EventChecksApply, PluginHandler: onApply},
	)
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tui-check-magicword <event>")
		os.Exit(1)
	}
	if err := factory.Run(pluggable.EventType(os.Args[1]), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// onChecks returns a single required text check asking for the magic word.
func onChecks(_ *pluggable.Event) pluggable.EventResponse {
	logf("offering the magic-word check")
	checks := []prereqs.Check{
		{
			ID:          checkID,
			Title:       "Magic word required",
			Message:     fmt.Sprintf("This node won't install until you say the magic word. (hint: it's %q)", magicWord),
			Severity:    prereqs.SeverityInfo,
			Required:    true,
			Prompt:      prereqs.PromptText,
			PromptText:  fmt.Sprintf("Type the magic word (%s):", magicWord),
			Placeholder: magicWord,
		},
	}
	return pluggable.EventResponse{Data: mustJSON(checks)}
}

// onApply validates the typed word. Only "jojo" succeeds; anything else fails,
// which (because the check is Required) blocks the install and prompts a retry.
func onApply(e *pluggable.Event) pluggable.EventResponse {
	var payload prereqs.ApplyPayload
	if err := json.Unmarshal([]byte(e.Data), &payload); err != nil {
		logf("invalid apply payload: %v", err)
		return pluggable.EventResponse{Data: mustJSON([]prereqs.ApplyResult{
			{ID: checkID, Success: false, Message: "invalid apply payload: " + err.Error()},
		})}
	}

	var results []prereqs.ApplyResult
	for _, d := range payload.Decisions {
		if d.ID != checkID {
			continue
		}
		got := strings.TrimSpace(d.Text)
		logf("user typed %q", got)
		if got == magicWord {
			results = append(results, prereqs.ApplyResult{ID: checkID, Success: true, Message: "the magic word is correct"})
		} else {
			results = append(results, prereqs.ApplyResult{
				ID:      checkID,
				Success: false,
				Message: fmt.Sprintf("%q is not the magic word — try again", got),
			})
		}
	}
	return pluggable.EventResponse{Data: mustJSON(results)}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
