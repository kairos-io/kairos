// tui-check-diskwipe is an example Kairos interactive-install check plugin
// (a "tui-check"). It participates in the installer's pre-installation sanity
// checks, a plugin set separate from the generic agent providers.
//
// It demonstrates the EventChecks / EventChecksApply round-trip defined in
// pkg/prereqs:
//
//   - On EventChecks it scans the system's disks and, for any disk that still
//     carries Kairos partition labels (COS_*) from a previous installation, it
//     returns a multi-select check letting the user pick which disks to wipe.
//   - On EventChecksApply it wipes the disks the user selected in the TUI
//     (wipefs) and reports the result.
//
// Build it and drop the resulting binary, named with the "tui-check-" prefix,
// into one of the check-plugin search paths (/system/tui-checks,
// /usr/local/system/tui-checks, or the agent's working directory):
//
//	go build -o tui-check-diskwipe ./tui-check-examples/tui-check-diskwipe
//	install -m0755 tui-check-diskwipe /usr/local/system/tui-checks/
//
// Anything this plugin writes to stdout/stderr is captured by go-pluggable into
// the EventResponse.Logs field and re-logged by the agent, so the log lines
// below show up in the agent's journal/log.
//
// NOTE: wiping a disk is destructive. The action only runs for disks the user
// explicitly selects on the prerequisites screen.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos-installer/prereqs"
	"github.com/kairos-io/kairos-sdk/ghw"
	"github.com/mudler/go-pluggable"
)

// logf writes a log line to stderr. It MUST resolve os.Stderr at call time:
// go-pluggable's factory.Run redirects os.Stdout/os.Stderr to a capture pipe
// only while the handler runs and puts that output in EventResponse.Logs, which
// the agent re-logs. Caching os.Stderr (e.g. log.SetOutput(os.Stderr) in main)
// would write to the real terminal and the agent would never see it.
func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "tui-check-diskwipe: "+format+"\n", a...)
}

// checkID is the single check this provider emits. The disks the user selects
// to wipe are carried in the multi-select answer (Decision.Selected).
const checkID = "diskwipe"

// kairosLabels are the partition filesystem labels a Kairos installation
// leaves behind. Their presence on a disk indicates leftover data.
var kairosLabels = map[string]bool{
	"COS_GRUB":       true,
	"COS_STATE":      true,
	"COS_RECOVERY":   true,
	"COS_OEM":        true,
	"COS_PERSISTENT": true,
	"COS_ACTIVE":     true,
	"COS_PASSIVE":    true,
	"COS_SYSTEM":     true,
}

func main() {
	factory := pluggable.NewPluginFactory(
		pluggable.FactoryPlugin{EventType: prereqs.EventChecks, PluginHandler: onChecks},
		pluggable.FactoryPlugin{EventType: prereqs.EventChecksApply, PluginHandler: onApply},
	)

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tui-check-diskwipe <event>")
		os.Exit(1)
	}

	// factory.Run dispatches to onChecks/onApply, capturing their stderr/stdout
	// into EventResponse.Logs (which the agent re-logs). The event name arrives
	// as argv[1] and the Event JSON on stdin.
	if err := factory.Run(pluggable.EventType(os.Args[1]), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// onChecks scans disks and, if any carry leftover Kairos partition labels,
// returns a single multi-select check letting the user choose which disks to
// wipe.
func onChecks(_ *pluggable.Event) pluggable.EventResponse {
	logf("scanning disks for leftover Kairos partitions")
	var options []prereqs.Option

	disks := ghw.GetDisks(ghw.NewPaths(""), nil)
	logf("found %d disk(s) on the system", len(disks))
	for _, disk := range disks {
		var found []string
		for _, p := range disk.Partitions {
			if kairosLabels[p.FilesystemLabel] {
				found = append(found, p.FilesystemLabel)
			}
		}
		if len(found) == 0 {
			continue
		}
		dev := "/dev/" + disk.Name
		logf("disk %s has leftover Kairos partitions: %s", dev, strings.Join(found, ", "))
		options = append(options, prereqs.Option{
			ID:    dev,
			Label: fmt.Sprintf("%s (%s)", dev, strings.Join(found, ", ")),
		})
	}

	var checks []prereqs.Check
	if len(options) > 0 {
		checks = append(checks, prereqs.Check{
			ID:         checkID,
			Title:      "Leftover Kairos data detected",
			Message:    fmt.Sprintf("%d disk(s) hold partitions from a previous installation", len(options)),
			Severity:   prereqs.SeverityWarning,
			Prompt:     prereqs.PromptMultiSelect,
			PromptText: "Select disks to wipe (all data on them will be destroyed):",
			Options:    options,
		})
	}
	logf("returning %d check(s)", len(checks))

	// Report results through the returned data, never through
	// EventResponse.Error: the agent aborts the whole process on a provider
	// error, which would kill the TUI.
	return pluggable.EventResponse{Data: mustJSON(checks)}
}

// onApply wipes the disks the user selected.
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
		logf("apply: %d disk(s) selected for wiping", len(d.Selected))
		for _, dev := range d.Selected {
			logf("wiping %s", dev)
			res := prereqs.ApplyResult{ID: checkID, Success: true, Message: "wiped " + dev}
			if err := wipe(dev); err != nil {
				logf("wipe %s failed: %v", dev, err)
				res.Success = false
				res.Message = err.Error()
			} else {
				logf("wiped %s successfully", dev)
			}
			results = append(results, res)
		}
	}

	return pluggable.EventResponse{Data: mustJSON(results)}
}

// wipe removes filesystem and partition-table signatures from a device.
func wipe(dev string) error {
	if out, err := exec.Command("wipefs", "-a", dev).CombinedOutput(); err != nil {
		return fmt.Errorf("wipefs %s: %w (%s)", dev, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
