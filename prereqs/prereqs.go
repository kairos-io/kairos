// Package prereqs defines the contract used by the interactive installer to
// surface pre-installation sanity checks and prerequisites to the user.
//
// The flow mirrors the existing interactive-install customization mechanism
// (see internal/agent/TUIcustomizationPage.go and the EventInteractiveInstall
// bus event): the agent publishes a bus event, provider plugins inspect the
// live system and respond with a list of checks. The TUI renders them on a
// dedicated screen *before* the installation starts. Each check may carry an
// interactive prompt (yes/no, free text, single- or multi-select). When the
// user answers, the agent publishes a second "apply" event carrying the user's
// decisions and the same provider executes the action (e.g. wiping the disks
// the user selected).
//
// Providers must report check failures through the returned Check data
// (Severity == SeverityError), NOT through the go-pluggable EventResponse.Error
// field: the agent bus installs a global handler that aborts the process when a
// provider returns an error, which would kill the TUI abruptly.
package prereqs

import (
	"encoding/json"
	"strings"

	"github.com/mudler/go-pluggable"
)

const (
	// EventChecks is published by the agent when the prerequisites screen
	// opens. Providers respond with a JSON-encoded []Check in
	// EventResponse.Data.
	EventChecks pluggable.EventType = "agent.interactive-install.checks"

	// EventChecksApply is published by the agent after the user has answered
	// the checks. The payload is an ApplyPayload. Providers act on the
	// decisions matching their own check IDs and respond with a JSON-encoded
	// []ApplyResult in EventResponse.Data.
	EventChecksApply pluggable.EventType = "agent.interactive-install.checks.apply"
)

// Severity levels for a Check.
const (
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Prompt types a check may carry. PromptNone is a display-only check.
const (
	PromptNone        = ""            // display only, no user input
	PromptConfirm     = "confirm"     // yes/no
	PromptText        = "text"        // free string input
	PromptSelect      = "select"      // pick exactly one of Options
	PromptMultiSelect = "multiselect" // pick zero or more of Options
)

// Option is a single choice for PromptSelect / PromptMultiSelect checks.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Check is a single prerequisite/sanity check returned by a provider.
type Check struct {
	// ID is a stable identifier. It is echoed back to the provider in the apply
	// round-trip so the provider knows which action to run. Providers should
	// namespace it, e.g. "diskwipe".
	ID string `json:"id"`
	// Title is a short label shown to the user.
	Title string `json:"title"`
	// Message is the detail shown to the user.
	Message string `json:"message"`
	// Severity is one of SeverityInfo, SeverityWarning or SeverityError.
	Severity string `json:"severity"`
	// Blocking, when true, prevents the installation from proceeding until the
	// check is satisfied (see Satisfied). A blocking display-only check (no
	// prompt) can never be satisfied from the TUI and represents a hard,
	// unrecoverable prerequisite failure (e.g. unsupported architecture).
	Blocking bool `json:"blocking"`
	// Required governs what happens when the check's apply action fails. When
	// true, an apply failure blocks the installation (the user can retry or
	// abort). When false (the default), an apply failure is surfaced as a
	// warning and the user may continue anyway.
	Required bool `json:"required"`

	// Prompt selects the interactive widget: PromptNone, PromptConfirm,
	// PromptText, PromptSelect or PromptMultiSelect.
	Prompt string `json:"prompt"`
	// PromptText is the question shown above/next to the widget.
	PromptText string `json:"promptText"`
	// Options are the choices for PromptSelect / PromptMultiSelect.
	Options []Option `json:"options"`
	// Default is the default answer: "yes"/"no" for confirm, the default text
	// for text, an Option ID for select, or comma-separated Option IDs for
	// multiselect.
	Default string `json:"default"`
	// Placeholder is the hint shown in an empty text input.
	Placeholder string `json:"placeholder"`
}

// Interactive reports whether the check asks the user for input.
func (c Check) Interactive() bool { return c.Prompt != PromptNone }

// IsError reports whether the check has error severity.
func (c Check) IsError() bool { return c.Severity == SeverityError }

// Answer holds the user's response to a check. The relevant field depends on
// the check's Prompt type.
type Answer struct {
	Confirmed bool     `json:"confirmed"` // PromptConfirm
	Text      string   `json:"text"`      // PromptText
	Selected  []string `json:"selected"`  // PromptSelect (len 1) / PromptMultiSelect
}

// Decision is the user's answer to a check, sent back to providers on apply.
// It echoes the prompt type so providers know which field to read.
type Decision struct {
	ID        string   `json:"id"`
	Prompt    string   `json:"prompt"`
	Confirmed bool     `json:"confirmed,omitempty"`
	Text      string   `json:"text,omitempty"`
	Selected  []string `json:"selected,omitempty"`
}

// ChecksPayload is published on EventChecks.
type ChecksPayload struct {
	// Config is the current (possibly partial) cloud-config, for providers
	// that want to take it into account. Providers mostly inspect the live
	// system instead.
	Config string `json:"config"`
}

// ApplyPayload is published on EventChecksApply.
type ApplyPayload struct {
	Decisions []Decision `json:"decisions"`
	Config    string     `json:"config"`
}

// ApplyResult is what a provider returns after applying decisions.
type ApplyResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ParseChecks unmarshals the JSON-encoded []Check returned by a provider in
// EventResponse.Data. An empty string yields a nil slice and no error.
func ParseChecks(data string) ([]Check, error) {
	if data == "" {
		return nil, nil
	}
	var checks []Check
	if err := json.Unmarshal([]byte(data), &checks); err != nil {
		return nil, err
	}
	return checks, nil
}

// ParseApplyResults unmarshals the JSON-encoded []ApplyResult returned by a
// provider. An empty string yields a nil slice and no error.
func ParseApplyResults(data string) ([]ApplyResult, error) {
	if data == "" {
		return nil, nil
	}
	var results []ApplyResult
	if err := json.Unmarshal([]byte(data), &results); err != nil {
		return nil, err
	}
	return results, nil
}

// HasPrompts reports whether any check asks the user for input.
func HasPrompts(checks []Check) bool {
	for _, c := range checks {
		if c.Interactive() {
			return true
		}
	}
	return false
}

// Satisfied reports whether the user's answer resolves the check. A
// display-only check is never satisfied. This is used to decide whether a
// blocking check still blocks the installation.
func Satisfied(c Check, a Answer) bool {
	switch c.Prompt {
	case PromptConfirm:
		return a.Confirmed
	case PromptText:
		return a.Text != ""
	case PromptSelect, PromptMultiSelect:
		return len(a.Selected) > 0
	default:
		return false
	}
}

// Blocker returns the first blocking check that is not yet satisfied and true,
// or a zero Check and false when nothing blocks the installation.
func Blocker(checks []Check, answers map[string]Answer) (Check, bool) {
	for _, c := range checks {
		if !c.Blocking {
			continue
		}
		if Satisfied(c, answers[c.ID]) {
			continue
		}
		return c, true
	}
	return Check{}, false
}

// BuildDecisions returns one Decision per interactive check, carrying the
// user's answer and echoing the prompt type.
func BuildDecisions(checks []Check, answers map[string]Answer) []Decision {
	var out []Decision
	for _, c := range checks {
		if !c.Interactive() {
			continue
		}
		a := answers[c.ID]
		out = append(out, Decision{
			ID:        c.ID,
			Prompt:    c.Prompt,
			Confirmed: a.Confirmed,
			Text:      a.Text,
			Selected:  a.Selected,
		})
	}
	return out
}

// ClassifyFailures splits the failed apply results into those belonging to a
// Required check and those belonging to an optional check, matched by ID.
// Results with Success == true are ignored. A required failure blocks the
// installation; an optional failure is a warning the user can continue past.
func ClassifyFailures(checks []Check, results []ApplyResult) (requiredFailed, optionalFailed []ApplyResult) {
	required := map[string]bool{}
	for _, c := range checks {
		required[c.ID] = c.Required
	}
	for _, r := range results {
		if r.Success {
			continue
		}
		if required[r.ID] {
			requiredFailed = append(requiredFailed, r)
		} else {
			optionalFailed = append(optionalFailed, r)
		}
	}
	return requiredFailed, optionalFailed
}

// DefaultAnswer returns the initial answer for a check based on its Default.
func DefaultAnswer(c Check) Answer {
	switch c.Prompt {
	case PromptConfirm:
		return Answer{Confirmed: c.Default == "yes" || c.Default == "true"}
	case PromptText:
		return Answer{Text: c.Default}
	case PromptSelect:
		if c.Default != "" {
			return Answer{Selected: []string{c.Default}}
		}
	case PromptMultiSelect:
		if c.Default != "" {
			var sel []string
			for part := range strings.SplitSeq(c.Default, ",") {
				if part = strings.TrimSpace(part); part != "" {
					sel = append(sel, part)
				}
			}
			return Answer{Selected: sel}
		}
	}
	return Answer{}
}
