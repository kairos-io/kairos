package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kairos-io/kairos-installer/prereqs"
	"github.com/mudler/go-pluggable"
)

// prerequisitesPage is the first screen of the interactive installer. It asks
// provider plugins (via the EventChecks bus event) to inspect the live system
// and return a set of pre-installation sanity checks / prerequisites. Each
// check may carry an interactive prompt: yes/no, free text, single-select or
// multi-select. The user reviews and answers them; on continue the answers are
// sent back to the providers (EventChecksApply) which execute their actions
// (e.g. wiping the disks the user selected) before the installation begins.
//
// When no provider returns any check the page auto-advances to disk selection,
// so it is invisible on systems without prerequisites providers.
type prerequisitesPage struct {
	checks   []prereqs.Check
	answers  map[string]prereqs.Answer  // by check ID
	texts    map[string]textinput.Model // by check ID, for PromptText
	fields   []prereqField              // flattened focusable items
	mgr      *pluggable.Manager         // dedicated tui-check plugin manager
	cursor   int
	loaded   bool
	applyErr string
	failure  string // failNone | failRequired | failOptional
	results  []prereqs.ApplyResult
}

const (
	failNone     = ""
	failRequired = "required"
	failOptional = "optional"
)

// prereqField is a single focusable item on the page. A confirm/select/text
// check maps to one field; a multiselect check maps to one field per option.
type prereqField struct {
	checkIdx int
	kind     string // "confirm" | "select" | "text" | "option"
	optIdx   int    // option index, for kind == "option"
}

func newPrerequisitesPage() *prerequisitesPage {
	return &prerequisitesPage{
		answers: map[string]prereqs.Answer{},
		texts:   map[string]textinput.Model{},
	}
}

func (p *prerequisitesPage) ID() string    { return "prerequisites" }
func (p *prerequisitesPage) Title() string { return "Prerequisites" }

func (p *prerequisitesPage) Help() string {
	switch p.failure {
	case failRequired:
		return "enter: retry • q/ctrl+c: quit"
	case failOptional:
		return "enter: continue anyway • r: retry • q/ctrl+c: quit"
	}
	if len(p.fields) == 0 {
		return "enter: continue"
	}
	return "↑/↓: move • ←/→ or space: change • enter: continue"
}

// Init gathers the checks from providers synchronously (go-pluggable runs
// plugins inline), mirroring the customization page. With no checks it emits a
// navigation message to skip straight to disk selection.
func (p *prerequisitesPage) Init() tea.Cmd {
	if !p.loaded {
		p.mgr = newCheckManager(*mainModel.log)
		checks, err := gatherChecks(p.mgr, *mainModel.log, "")
		if err != nil {
			mainModel.log.Logger.Warn().Err(err).Msg("gathering prerequisites checks")
		}
		p.checks = checks
		p.buildFields()
		p.loaded = true
		mainModel.log.Logger.Debug().Int("checks", len(checks)).Int("fields", len(p.fields)).Msg("Prerequisites gathered")
	}

	if len(p.checks) == 0 {
		return func() tea.Msg { return GoToPageMsg{PageID: "disk_selection"} }
	}
	return p.syncFocus()
}

// buildFields flattens the checks into focusable fields and seeds default
// answers and text inputs.
func (p *prerequisitesPage) buildFields() {
	p.fields = nil
	for i, c := range p.checks {
		p.answers[c.ID] = prereqs.DefaultAnswer(c)
		switch c.Prompt {
		case prereqs.PromptConfirm:
			p.fields = append(p.fields, prereqField{checkIdx: i, kind: "confirm"})
		case prereqs.PromptSelect:
			p.fields = append(p.fields, prereqField{checkIdx: i, kind: "select"})
		case prereqs.PromptText:
			ti := textinput.New()
			ti.Placeholder = c.Placeholder
			ti.SetValue(p.answers[c.ID].Text)
			ti.Width = 40
			p.texts[c.ID] = ti
			p.fields = append(p.fields, prereqField{checkIdx: i, kind: "text"})
		case prereqs.PromptMultiSelect:
			for oi := range c.Options {
				p.fields = append(p.fields, prereqField{checkIdx: i, kind: "option", optIdx: oi})
			}
		}
	}
}

func (p *prerequisitesPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}

	var cur *prereqField
	if p.cursor >= 0 && p.cursor < len(p.fields) {
		cur = &p.fields[p.cursor]
	}

	// Navigation keys that always apply, even inside a text field.
	switch km.String() {
	case "up":
		p.moveCursor(-1)
		return p, p.syncFocus()
	case "down", "tab":
		p.moveCursor(1)
		return p, p.syncFocus()
	case "shift+tab":
		p.moveCursor(-1)
		return p, p.syncFocus()
	case "enter":
		// After an optional-action failure, enter means "continue anyway".
		if p.failure == failOptional {
			return p, func() tea.Msg { return GoToPageMsg{PageID: "disk_selection"} }
		}
		return p.proceed()
	}

	// In a failure state, 'r' retries the apply (re-runs every decision) —
	// but not while a text field is focused, where 'r' is a literal character.
	if p.failure != failNone && (km.String() == "r" || km.String() == "R") &&
		!(cur != nil && cur.kind == "text") {
		return p.proceed()
	}

	// Text field: route everything else to the input.
	if cur != nil && cur.kind == "text" {
		c := p.checks[cur.checkIdx]
		ti := p.texts[c.ID]
		var cmd tea.Cmd
		ti, cmd = ti.Update(msg)
		p.texts[c.ID] = ti
		a := p.answers[c.ID]
		a.Text = ti.Value()
		p.answers[c.ID] = a
		// Editing an answer invalidates a previous failure.
		p.clearFailure()
		return p, cmd
	}

	// Non-text fields.
	switch km.String() {
	case "k":
		p.moveCursor(-1)
		return p, p.syncFocus()
	case "j":
		p.moveCursor(1)
		return p, p.syncFocus()
	case "left", "h":
		p.control(cur, -1)
	case "right", "l":
		p.control(cur, +1)
	case " ":
		p.control(cur, 0)
	}
	return p, nil
}

func (p *prerequisitesPage) moveCursor(delta int) {
	if len(p.fields) == 0 {
		return
	}
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor > len(p.fields)-1 {
		p.cursor = len(p.fields) - 1
	}
}

// control changes the value of the focused non-text field. dir -1/+1 are
// directional (left/right); 0 means toggle (space).
func (p *prerequisitesPage) control(cur *prereqField, dir int) {
	if cur == nil {
		return
	}
	// Changing an answer invalidates a previous failure.
	p.clearFailure()
	c := p.checks[cur.checkIdx]
	a := p.answers[c.ID]

	switch cur.kind {
	case "confirm":
		switch dir {
		case -1:
			a.Confirmed = false
		case 1:
			a.Confirmed = true
		default:
			a.Confirmed = !a.Confirmed
		}
	case "select":
		// Single-select: ←/→ cycle through the options, keeping exactly one
		// (or zero) selected.
		next := cycleOption(c.Options, a.Selected, dir)
		if next == "" {
			a.Selected = nil
		} else {
			a.Selected = []string{next}
		}
	case "option":
		// Multi-select: each option is its own focusable field. → selects,
		// ← deselects, space toggles — independently per option, so any
		// number of options can be selected at once.
		id := c.Options[cur.optIdx].ID
		if dir == 1 {
			a.Selected = addUnique(a.Selected, id)
		} else if dir == -1 {
			a.Selected = remove(a.Selected, id)
		} else { // toggle
			if contains(a.Selected, id) {
				a.Selected = remove(a.Selected, id)
			} else {
				a.Selected = addUnique(a.Selected, id)
			}
		}
	}
	p.answers[c.ID] = a
}

// syncFocus focuses the text input under the cursor (if any) and blurs the
// rest. Returns the blink command for the focused input.
func (p *prerequisitesPage) syncFocus() tea.Cmd {
	for id, ti := range p.texts {
		ti.Blur()
		p.texts[id] = ti
	}
	if p.cursor >= 0 && p.cursor < len(p.fields) {
		f := p.fields[p.cursor]
		if f.kind == "text" {
			c := p.checks[f.checkIdx]
			ti := p.texts[c.ID]
			cmd := ti.Focus()
			p.texts[c.ID] = ti
			return cmd
		}
	}
	return nil
}

// clearFailure resets any pending failure state so the user can retry from a
// clean slate after changing an answer.
func (p *prerequisitesPage) clearFailure() {
	p.failure = failNone
	p.applyErr = ""
	p.results = nil
}

// advance returns the command that moves on to disk selection.
func (p *prerequisitesPage) advance() tea.Cmd {
	return func() tea.Msg { return GoToPageMsg{PageID: "disk_selection"} }
}

// proceed validates blockers, applies the user's decisions and advances to
// disk selection.
//
//   - An unsatisfied blocking check keeps the user on the page (failRequired).
//   - A failed Required action keeps the user on the page; enter retries
//     (failRequired).
//   - A failed optional action is surfaced as a warning; the user may continue
//     anyway with enter or retry with 'r' (failOptional).
func (p *prerequisitesPage) proceed() (Page, tea.Cmd) {
	p.clearFailure()

	if c, blocked := prereqs.Blocker(p.checks, p.answers); blocked {
		p.failure = failRequired
		p.applyErr = fmt.Sprintf("Cannot continue: %s — %s", c.Title, c.Message)
		return p, nil
	}

	decisions := prereqs.BuildDecisions(p.checks, p.answers)
	if len(decisions) > 0 {
		results, err := applyDecisions(p.mgr, *mainModel.log, decisions, "")
		if err != nil {
			// A transport/exec failure is unrecoverable from here: block.
			p.failure = failRequired
			p.applyErr = fmt.Sprintf("Failed applying prerequisites: %s", err.Error())
			return p, nil
		}
		p.results = results

		reqFail, optFail := prereqs.ClassifyFailures(p.checks, results)
		if len(reqFail) > 0 {
			p.failure = failRequired
			p.applyErr = "Required action(s) failed. Fix the issue and retry."
			return p, nil
		}
		if len(optFail) > 0 {
			p.failure = failOptional
			p.applyErr = "Some optional action(s) failed. Press enter to continue or r to retry."
			return p, nil
		}
	}

	return p, p.advance()
}

func (p *prerequisitesPage) View() string {
	s := "Pre-installation checks\n\n"
	if !p.loaded {
		return s + "Checking prerequisites...\n"
	}

	// Build the inner content of each box first, then render every box at one
	// uniform width: the widest content, capped to the screen. This keeps the
	// boxes aligned without stretching them across the whole screen.
	inners := make([]string, len(p.checks))
	contentW := 0
	for i, c := range p.checks {
		inners[i] = p.checkInner(i, c)
		if lw := lipgloss.Width(inners[i]); lw > contentW {
			contentW = lw
		}
	}
	w, _ := effectiveSize(mainModel.width, mainModel.height)
	if maxContent := w - 10; contentW > maxContent { // leave room for border + padding + page margin
		contentW = maxContent
	}
	for i, c := range p.checks {
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(severityColor(c.Severity)).
			Background(kairosBg).
			Padding(0, 1).
			Width(contentW + 2). // +2 for the horizontal padding
			Render(inners[i])
		s += box + "\n"
	}

	if len(p.results) > 0 {
		s += "\nResults:\n"
		okStyle := lipgloss.NewStyle().Foreground(kairosAccent)
		failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
		for _, r := range p.results {
			mark := okStyle.Render(checkMark)
			if !r.Success {
				mark = failStyle.Render("✗")
			}
			line := fmt.Sprintf("  %s %s", mark, r.ID)
			if r.Message != "" {
				line += ": " + r.Message
			}
			s += line + "\n"
		}
	}

	if p.applyErr != "" {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
		if p.failure == failOptional {
			style = lipgloss.NewStyle().Foreground(kairosAccent).Bold(true)
		}
		s += "\n" + style.Render(p.applyErr) + "\n"
	}
	return s
}

// renderPrompt renders the interactive widget for a check, marking the focused
// field.
func (p *prerequisitesPage) renderPrompt(checkIdx int, c prereqs.Check) string {
	a := p.answers[c.ID]
	prompt := c.PromptText
	cur := func(kind string, opt int) string {
		if p.focused(checkIdx, kind, opt) {
			return lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		return " "
	}
	on := lipgloss.NewStyle().Foreground(kairosAccent)

	switch c.Prompt {
	case prereqs.PromptConfirm:
		yes, no := "( ) yes", "( ) no"
		if a.Confirmed {
			yes = on.Render("(•) yes")
		} else {
			no = on.Render("(•) no")
		}
		return fmt.Sprintf("  %s %s  %s  %s\n", cur("confirm", 0), prompt, yes, no)
	case prereqs.PromptSelect:
		sel := ""
		if len(a.Selected) > 0 {
			sel = optionLabel(c.Options, a.Selected[0])
		}
		return fmt.Sprintf("  %s %s  < %s >\n", cur("select", 0), prompt, on.Render(sel))
	case prereqs.PromptText:
		ti := p.texts[c.ID]
		return fmt.Sprintf("  %s %s %s\n", cur("text", 0), prompt, ti.View())
	case prereqs.PromptMultiSelect:
		out := ""
		if prompt != "" {
			out += "  " + prompt + "\n"
		}
		for oi, opt := range c.Options {
			box := "[ ]"
			if contains(a.Selected, opt.ID) {
				box = on.Render("[" + checkMark + "]")
			}
			out += fmt.Sprintf("    %s %s %s\n", cur("option", oi), box, opt.Label)
		}
		return out
	default:
		return ""
	}
}

// checkInner builds the content shown inside a check's box: the name on the
// first line, then the message, then (with a blank line of breathing room) the
// input widget.
func (p *prerequisitesPage) checkInner(checkIdx int, c prereqs.Check) string {
	title := fmt.Sprintf("[%s] %s", strings.ToUpper(severityName(c.Severity)), c.Title)
	inner := severityStyle(c.Severity).Bold(true).Render(title)

	if c.Message != "" {
		inner += "\n" + c.Message
	}
	if prompt := p.renderPrompt(checkIdx, c); prompt != "" {
		inner += "\n\n" + strings.TrimRight(prompt, "\n")
	}
	return inner
}

// focused reports whether the cursor is on the given field.
func (p *prerequisitesPage) focused(checkIdx int, kind string, opt int) bool {
	if p.cursor < 0 || p.cursor >= len(p.fields) {
		return false
	}
	f := p.fields[p.cursor]
	return f.checkIdx == checkIdx && f.kind == kind && (kind != "option" || f.optIdx == opt)
}

// --- small helpers ---

func severityName(s string) string {
	if s == "" {
		return prereqs.SeverityInfo
	}
	return s
}

// severityColor maps a severity to a distinct color: blue (info), orange
// (warning), red (error).
func severityColor(severity string) lipgloss.Color {
	switch severity {
	case prereqs.SeverityError:
		return lipgloss.Color("#ff5555")
	case prereqs.SeverityWarning:
		return kairosAccent
	default:
		return lipgloss.Color("#5fafff")
	}
}

func severityStyle(severity string) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(severityColor(severity))
	if severity == prereqs.SeverityError {
		s = s.Bold(true)
	}
	return s
}

func optionLabel(options []prereqs.Option, id string) string {
	for _, o := range options {
		if o.ID == id {
			return o.Label
		}
	}
	return id
}

// cycleOption returns the option ID after moving dir steps from the currently
// selected one. With nothing selected, a forward move picks the first option.
func cycleOption(options []prereqs.Option, selected []string, dir int) string {
	if len(options) == 0 {
		return ""
	}
	idx := 0
	if len(selected) > 0 {
		for i, o := range options {
			if o.ID == selected[0] {
				idx = i
				break
			}
		}
		idx += dir
	} else if dir < 0 {
		idx = len(options) - 1
	}
	if idx < 0 {
		idx = len(options) - 1
	}
	if idx >= len(options) {
		idx = 0
	}
	return options[idx].ID
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func addUnique(s []string, v string) []string {
	if contains(s, v) {
		return s
	}
	return append(s, v)
}

func remove(s []string, v string) []string {
	out := s[:0:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
