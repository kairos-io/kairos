package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	sdkBus "github.com/kairos-io/kairos/v4/sdk/bus"

	"github.com/kairos-io/kairos/v4/installer/internal/disks"
	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// visibleChoiceRows is how many entries a choice list shows at once. The
// model truncates a page to the terminal height, and a serial console reports
// none, so the fallback 80x24 leaves about ten lines for the page body.
const visibleChoiceRows = 7

// fieldWidget draws and edits one wizard.Field.
type fieldWidget interface {
	Update(tea.KeyMsg) tea.Cmd
	View(focused bool, width int) string
	// Value is the field's value in the wizard's string encoding.
	Value() string
	Help() string
	// Load shows the current answer, so a revisited step is not blank.
	Load(a wizard.Answers)
	// Focus and Blur toggle a text cursor, where there is one.
	Focus() tea.Cmd
	Blur()
	// Wants reports whether the widget uses this key itself, so enter in a
	// list adds an entry instead of submitting the step.
	Wants(tea.KeyMsg) bool
}

// widgetFor has one constructor per wizard.Kind. The drift test fails when a
// Kind has no entry here.
var widgetFor = map[wizard.Kind]func(wizard.Field) fieldWidget{
	wizard.KindText:        func(f wizard.Field) fieldWidget { return newTextWidget(f, false) },
	wizard.KindPassword:    func(f wizard.Field) fieldWidget { return newTextWidget(f, true) },
	wizard.KindChoice:      func(f wizard.Field) fieldWidget { return newChoiceWidget(f, false) },
	wizard.KindMultiChoice: func(f wizard.Field) fieldWidget { return newChoiceWidget(f, true) },
	wizard.KindBool:        func(f wizard.Field) fieldWidget { return &boolWidget{field: f} },
	wizard.KindList:        func(f wizard.Field) fieldWidget { return newListWidget(f) },
}

// stepPage renders any wizard.Step.
type stepPage struct {
	step    wizard.Step
	widgets []fieldWidget
	focus   int
	errs    map[string]string
	// pending is set on the extensions step until its choices are fetched,
	// and loading while the fetch runs.
	pending, loading bool
}

// loadingExtensions is what the extensions step shows while it reads the
// live media and the catalog.
const loadingExtensions = "Looking for extensions on the live media and in the catalog..."

// extensionsLoadedMsg carries the extensions step with its choices.
type extensionsLoadedMsg struct{ step wizard.Step }

// noExtensionsEnv is the env with the extension fetch taken out, for the
// steps the installer resolves before its first frame.
type noExtensionsEnv struct{ wizard.Env }

func (noExtensionsEnv) Extensions(context.Context) ([]wizard.Choice, error) { return nil, nil }

// fetchExtensions reads the catalog off the event loop, once.
func (p *stepPage) fetchExtensions() tea.Cmd {
	if p.loading {
		return nil
	}
	p.loading = true
	env := wizardEnv
	return func() tea.Msg {
		return extensionsLoadedMsg{step: wizard.ExtensionsStep(context.Background(), env)}
	}
}

// loaded puts the fetched step on the page and in mainModel.steps, which
// Apply checks the picks against, and shows the saved picks on it.
func (p *stepPage) loaded(step wizard.Step) tea.Cmd {
	p.step, p.pending, p.loading = step, false, false
	p.widgets = nil
	for _, f := range step.Fields {
		w := widgetFor[f.Kind](f)
		w.Load(mainModel.answers)
		w.Blur()
		p.widgets = append(p.widgets, w)
	}
	for i := range mainModel.steps {
		if mainModel.steps[i].ID == step.ID {
			mainModel.steps[i] = step
		}
	}
	p.focus = 0
	if len(p.widgets) == 0 || mainModel.currentPageID != p.ID() {
		return nil
	}
	return p.widgets[0].Focus()
}

func newStepPage(step wizard.Step) *stepPage {
	p := &stepPage{step: step, errs: map[string]string{}}
	for i, f := range step.Fields {
		w := widgetFor[f.Kind](f)
		// A widget is built ready to take keys; only the first field of a
		// page keeps that until Init or tab moves the focus.
		if i > 0 {
			w.Blur()
		}
		p.widgets = append(p.widgets, w)
	}
	return p
}

func (p *stepPage) ID() string    { return p.step.ID }
func (p *stepPage) Title() string { return p.step.Title }

func (p *stepPage) Help() string {
	if p.pending {
		return "esc: back"
	}
	h := "tab: next field • enter: save • esc: back"
	if len(p.widgets) > 0 {
		h = p.widgets[p.focus].Help() + " • " + h
	}
	if p.step.ID == wizard.StepDisk {
		h += " • ctrl+d: collect debug logs"
	}
	return h
}

// diskOnlyEnv answers the disk scan from the real env and nothing else, so
// re-reading the disk step does not re-run the network catalog fetch. It
// keeps the scan error, which the step only shows as a notice.
type diskOnlyEnv struct {
	wizard.Env
	err *error
}

func (e diskOnlyEnv) Disks() ([]disks.Disk, error) {
	found, err := e.Env.Disks()
	*e.err = err
	return found, err
}
func (diskOnlyEnv) Extensions(context.Context) ([]wizard.Choice, error) { return nil, nil }
func (diskOnlyEnv) ProviderPrompts() []sdkBus.YAMLPrompt                { return nil }
func (diskOnlyEnv) AdvancedDisabled() bool                              { return true }

// rescanDisks rebuilds the disk step, because a prerequisites plugin such as
// wipefs can change the disks between two visits (#4260). A failed scan
// keeps the list the operator was looking at rather than blanking it.
func (p *stepPage) rescanDisks() {
	var scanErr error
	fresh, ok := wizard.StepByID(wizard.Steps(context.Background(), diskOnlyEnv{Env: wizardEnv, err: &scanErr}), wizard.StepDisk)
	if !ok || (scanErr != nil && len(p.step.Fields) > 0 && len(p.step.Fields[0].Choices) > 0) {
		return
	}
	p.step = fresh
	p.widgets[0] = widgetFor[wizard.KindChoice](fresh.Fields[0])
	for i := range mainModel.steps {
		if mainModel.steps[i].ID == wizard.StepDisk {
			mainModel.steps[i] = fresh
		}
	}
}

func (p *stepPage) Init() tea.Cmd {
	if p.step.ID == wizard.StepDisk {
		p.rescanDisks()
	}
	for _, w := range p.widgets {
		w.Load(mainModel.answers)
		w.Blur()
	}
	p.focus, p.errs = 0, map[string]string{}
	if p.pending {
		return p.fetchExtensions()
	}
	if len(p.widgets) == 0 {
		return nil
	}
	return p.widgets[0].Focus()
}

func (p *stepPage) values() map[string]string {
	v := map[string]string{}
	for i, f := range p.step.Fields {
		val := p.widgets[i].Value()
		if f.Kind == wizard.KindPassword {
			pw, confirm, _ := strings.Cut(val, "\n")
			v[f.ID], v[f.ID+wizard.ConfirmSuffix] = pw, confirm
			continue
		}
		v[f.ID] = val
	}
	return v
}

func (p *stepPage) move(delta int) tea.Cmd {
	p.widgets[p.focus].Blur()
	p.focus = (p.focus + delta + len(p.widgets)) % len(p.widgets)
	return p.widgets[p.focus].Focus()
}

// CapturesKey keeps q, esc and ctrl+d from the model when the focused widget
// uses them: a q typed into a text field, esc closing a choice filter,
// ctrl+d removing a list entry.
func (p *stepPage) CapturesKey(k tea.KeyMsg) bool {
	if p.pending || len(p.widgets) == 0 {
		return false
	}
	w := p.widgets[p.focus]
	if w.Wants(k) {
		return true
	}
	if k.Type != tea.KeyRunes {
		return false
	}
	switch w.(type) {
	case *textWidget, *listWidget:
		return true
	}
	return false
}

func (p *stepPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	// Nothing to save until the choices are here; esc still goes back.
	if !ok || p.pending || len(p.widgets) == 0 {
		return p, nil
	}
	w := p.widgets[p.focus]
	if w.Wants(k) {
		return p, w.Update(k)
	}
	switch k.String() {
	case "tab":
		return p, p.move(1)
	case "shift+tab":
		return p, p.move(-1)
	case "enter":
		// enter saves the step on its last field and moves on from the
		// others, so a second list on the page is never skipped.
		if p.focus < len(p.widgets)-1 {
			return p, p.move(1)
		}
		return p, p.submit()
	}
	return p, w.Update(k)
}

func (p *stepPage) submit() tea.Cmd {
	answers, errs := wizard.Apply(mainModel.steps, mainModel.answers, p.step.ID, p.values())
	p.errs = map[string]string{}
	if len(errs) > 0 {
		for _, e := range errs {
			p.errs[e.Field] = e.Message
		}
		return nil
	}
	mainModel.answers = answers
	// The hash is in the answers now; do not keep the plaintext around.
	for _, w := range p.widgets {
		if t, ok := w.(*textWidget); ok && t.password {
			t.input.SetValue("")
			t.confirm.SetValue("")
		}
	}
	next := "customization"
	if p.step.ID == wizard.StepDisk {
		next = "install_options"
	}
	return func() tea.Msg { return GoToPageMsg{PageID: next} }
}

// Configured ticks the step on the customization menu.
func (p *stepPage) Configured() bool {
	a := mainModel.answers
	switch p.step.ID {
	case wizard.StepUser:
		return a.Username != ""
	case wizard.StepSSHKeys:
		return len(a.SSHKeys) > 0
	case wizard.StepHostname:
		return a.Hostname != ""
	case wizard.StepLocale:
		return a.Timezone != "" || a.Keymap != ""
	case wizard.StepExtensions:
		return len(a.Extensions) > 0
	case wizard.StepProvider:
		return len(a.Provider) > 0
	}
	return false
}

func (p *stepPage) View() string {
	width, _ := effectiveSize(mainModel.width, mainModel.height)
	warn := lipgloss.NewStyle().Foreground(kairosHighlight2)
	s := p.step.Title + "\n"
	if p.step.Help != "" {
		s += p.step.Help + "\n"
	}
	if p.pending {
		return s + "\n" + loadingExtensions + "\n"
	}
	if p.step.Notice != "" {
		s += warn.Render(p.step.Notice) + "\n"
	}
	if p.step.ID == wizard.StepDisk {
		s += warn.Render("WARNING: All data on the selected disk will be DESTROYED!") + "\n"
	}
	s += "\n"
	for i, f := range p.step.Fields {
		if len(p.step.Fields) > 1 || f.Kind != wizard.KindChoice {
			s += f.Label + ":\n"
		}
		s += p.widgets[i].View(i == p.focus, width-8) + "\n"
		if msg := p.errs[f.ID]; msg != "" {
			s += warn.Render(msg) + "\n"
		}
		if msg := p.errs[""]; msg != "" && i == 0 {
			s += warn.Render(msg) + "\n"
		}
	}
	return s
}

// ---- text and password ----

type textWidget struct {
	field    wizard.Field
	input    textinput.Model
	confirm  textinput.Model // password only
	password bool
	second   bool // focus is on confirm
}

func newTextWidget(f wizard.Field, password bool) *textWidget {
	mk := func(ph string) textinput.Model {
		t := textinput.New()
		t.Placeholder = ph
		t.Width = 60
		if password {
			t.EchoMode = textinput.EchoPassword
		}
		return t
	}
	w := &textWidget{field: f, password: password, input: mk(f.Placeholder)}
	if password {
		w.confirm = mk("type it again")
	}
	// textinput drops every key while blurred, so a widget starts focused.
	w.input.Focus()
	return w
}

func (w *textWidget) Load(a wizard.Answers) {
	w.input.SetValue(answerFor(a, w.field))
	if w.password {
		w.input.SetValue("")
		w.confirm.SetValue("")
	}
}
func (w *textWidget) Focus() tea.Cmd { w.second = false; return w.input.Focus() }
func (w *textWidget) Blur()          { w.input.Blur(); w.confirm.Blur() }
func (w *textWidget) Wants(k tea.KeyMsg) bool {
	// Inside a password, tab and enter move to the confirmation before
	// leaving, so enter never submits with the confirmation still empty.
	return w.password && !w.second && (k.String() == "tab" || k.String() == "enter")
}
func (w *textWidget) Update(k tea.KeyMsg) tea.Cmd {
	if w.password && !w.second && (k.String() == "tab" || k.String() == "enter") {
		w.second = true
		w.input.Blur()
		return w.confirm.Focus()
	}
	var cmd tea.Cmd
	if w.second {
		w.confirm, cmd = w.confirm.Update(k)
	} else {
		w.input, cmd = w.input.Update(k)
	}
	return cmd
}
func (w *textWidget) View(focused bool, _ int) string {
	s := cursor(focused && !w.second) + " " + w.input.View()
	if w.password {
		s += "\n" + cursor(focused && w.second) + " " + w.confirm.View()
	}
	if w.field.Help != "" {
		s += "\n  " + lipgloss.NewStyle().Faint(true).Render(w.field.Help)
	}
	return s
}
func (w *textWidget) Value() string {
	if w.password {
		if w.input.Value() == "" && w.confirm.Value() == "" {
			return ""
		}
		return w.input.Value() + "\n" + w.confirm.Value()
	}
	return w.input.Value()
}
func (w *textWidget) Help() string {
	if w.password {
		return "type the password, enter, type it again"
	}
	return "type to edit"
}

// ---- choice and multichoice ----

type choiceWidget struct {
	field wizard.Field
	// options is what the widget offers: the field's choices, after a
	// "(leave unset)" row on an optional single choice.
	options []wizard.Choice
	multi   bool
	cursor  int
	// unfiltered is where the cursor was when the filter opened.
	unfiltered int
	selected   map[string]bool
	filter     textinput.Model
	filtering  bool
}

// unsetLabel is the row an optional single choice starts on, so an
// unanswered list never submits its first entry by accident.
const unsetLabel = "(leave unset)"

func newChoiceWidget(f wizard.Field, multi bool) *choiceWidget {
	t := textinput.New()
	t.Prompt = "/"
	w := &choiceWidget{field: f, multi: multi, selected: map[string]bool{}, filter: t, options: f.Choices}
	if !multi && !f.Required && !containsValue(f.Choices, "") {
		w.options = append([]wizard.Choice{{Value: "", Label: unsetLabel}}, f.Choices...)
	}
	return w
}

func (w *choiceWidget) visible() []wizard.Choice {
	q := strings.ToLower(w.filter.Value())
	if q == "" {
		return w.options
	}
	var out []wizard.Choice
	for _, c := range w.options {
		if strings.Contains(strings.ToLower(c.Label), q) && !containsValue(out, c.Value) {
			out = append(out, c)
		}
	}
	return out
}

func containsValue(cs []wizard.Choice, v string) bool {
	for _, c := range cs {
		if c.Value == v {
			return true
		}
	}
	return false
}

func (w *choiceWidget) Load(a wizard.Answers) {
	w.filtering = false
	w.filter.SetValue("")
	w.filter.Blur()
	w.cursor = 0
	w.selected = map[string]bool{}
	cur := answerFor(a, w.field)
	if cur == "" && !w.multi {
		cur = w.field.Default
	}
	for _, v := range strings.Split(cur, "\n") {
		w.selected[v] = true
	}
	for i, c := range w.options {
		if w.selected[c.Value] {
			w.cursor = i
			break
		}
	}
}
func (w *choiceWidget) Focus() tea.Cmd { return nil }
func (w *choiceWidget) Blur()          {}
func (w *choiceWidget) Wants(k tea.KeyMsg) bool {
	return w.filtering && k.String() != "enter" && k.String() != "tab"
}
func (w *choiceWidget) Update(k tea.KeyMsg) tea.Cmd {
	vis := w.visible()
	if w.filtering {
		return w.updateFiltering(k, vis)
	}
	switch k.String() {
	case "/":
		w.filtering = true
		w.unfiltered = w.cursor
		return w.filter.Focus()
	case "up", "k":
		if w.cursor > 0 {
			w.cursor--
		}
	case "down", "j":
		if w.cursor < len(vis)-1 {
			w.cursor++
		}
	case " ", "x":
		if w.cursor < len(vis) {
			v := vis[w.cursor].Value
			if w.multi {
				w.selected[v] = !w.selected[v]
			} else {
				w.selected = map[string]bool{v: true}
			}
		}
	}
	return nil
}

// updateFiltering handles a key while the filter has the keyboard. The
// arrows still move, space still toggles a multichoice, and esc leaves the
// filter with the cursor on the entry it was on.
func (w *choiceWidget) updateFiltering(k tea.KeyMsg, vis []wizard.Choice) tea.Cmd {
	switch k.String() {
	case "esc":
		w.filtering = false
		w.filter.Blur()
		cur := w.unfiltered
		if w.cursor < len(vis) {
			cur = indexOfValue(w.options, vis[w.cursor].Value)
		}
		w.filter.SetValue("")
		w.cursor = cur
		return nil
	case "up":
		if w.cursor > 0 {
			w.cursor--
		}
		return nil
	case "down":
		if w.cursor < len(vis)-1 {
			w.cursor++
		}
		return nil
	case " ":
		if w.multi {
			if w.cursor < len(vis) {
				v := vis[w.cursor].Value
				w.selected[v] = !w.selected[v]
			}
			return nil
		}
	}
	before := w.filter.Value()
	var cmd tea.Cmd
	w.filter, cmd = w.filter.Update(k)
	if w.filter.Value() != before {
		w.cursor = 0
	}
	return cmd
}

func indexOfValue(cs []wizard.Choice, v string) int {
	for i, c := range cs {
		if c.Value == v {
			return i
		}
	}
	return 0
}

func (w *choiceWidget) Value() string {
	if !w.multi {
		vis := w.visible()
		if w.cursor < len(vis) {
			return vis[w.cursor].Value
		}
		return ""
	}
	var out []string
	for _, c := range w.options {
		if w.selected[c.Value] && !contains(out, c.Value) {
			out = append(out, c.Value)
		}
	}
	return strings.Join(out, "\n")
}
func (w *choiceWidget) View(focused bool, _ int) string {
	vis := w.visible()
	var b strings.Builder
	if w.filtering || w.filter.Value() != "" {
		b.WriteString("  " + w.filter.View() + "\n")
	}
	if len(vis) == 0 {
		if w.filter.Value() != "" {
			return b.String() + "  (nothing matches)"
		}
		return "  (nothing to choose from)"
	}
	first, last := visibleWindow(w.cursor, len(vis), visibleChoiceRows)
	if first > 0 {
		fmt.Fprintf(&b, "  ... %d more above\n", first)
	}
	for i := first; i < last; i++ {
		c := vis[i]
		mark := ""
		if w.multi {
			mark = "[ ] "
			if w.selected[c.Value] {
				mark = "[" + lipgloss.NewStyle().Foreground(kairosAccent).Render(checkMark) + "] "
			}
		}
		line := cursor(focused && i == w.cursor) + " " + mark + c.Label
		if c.Detail != "" {
			line += " (" + c.Detail + ")"
		}
		b.WriteString(line + "\n")
	}
	if last < len(vis) {
		fmt.Fprintf(&b, "  ... %d more below\n", len(vis)-last)
	}
	return strings.TrimRight(b.String(), "\n")
}
func (w *choiceWidget) Help() string {
	h := "↑/↓: move"
	if w.multi {
		h += " • space: toggle"
	}
	if len(w.field.Choices) > visibleChoiceRows {
		h += " • /: filter"
	}
	return h
}

// ---- bool ----

type boolWidget struct {
	field wizard.Field
	on    bool
}

func (w *boolWidget) Load(a wizard.Answers) { w.on = answerFor(a, w.field) == "true" }
func (w *boolWidget) Focus() tea.Cmd        { return nil }
func (w *boolWidget) Blur()                 {}
func (w *boolWidget) Wants(tea.KeyMsg) bool { return false }
func (w *boolWidget) Update(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case " ", "left", "right", "h", "l", "y", "n":
		w.on = k.String() == "y" || (k.String() != "n" && !w.on)
	}
	return nil
}
func (w *boolWidget) Value() string {
	if w.on {
		return "true"
	}
	return "false"
}
func (w *boolWidget) View(focused bool, _ int) string {
	acc := lipgloss.NewStyle().Foreground(kairosAccent).Bold(true)
	yes, no := " Yes ", acc.Render("[No]")
	if w.on {
		yes, no = acc.Render("[Yes]"), " No "
	}
	return cursor(focused) + " " + yes + " " + no
}
func (w *boolWidget) Help() string { return "space: toggle" }

// ---- list ----

type listWidget struct {
	field  wizard.Field
	items  []string
	input  textinput.Model
	cursor int // len(items) is the input row
}

func newListWidget(f wizard.Field) *listWidget {
	t := textinput.New()
	t.Placeholder = f.Placeholder
	t.Width = 60
	// textinput drops every key while blurred, so a widget starts focused.
	t.Focus()
	return &listWidget{field: f, input: t}
}
func (w *listWidget) Load(a wizard.Answers) {
	w.items = nil
	if v := answerFor(a, w.field); v != "" {
		w.items = strings.Split(v, "\n")
	}
	w.cursor = len(w.items)
}
func (w *listWidget) Focus() tea.Cmd { w.cursor = len(w.items); return w.input.Focus() }
func (w *listWidget) Blur()          { w.input.Blur() }
func (w *listWidget) Wants(k tea.KeyMsg) bool {
	switch k.String() {
	case "enter":
		// enter on a typed key adds it; enter on an empty input submits the step.
		return strings.TrimSpace(w.input.Value()) != ""
	case "delete", "ctrl+d":
		// On an entry these remove it, rather than opening the debug bundle.
		return w.cursor < len(w.items)
	}
	return false
}
func (w *listWidget) Update(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "enter":
		if v := strings.TrimSpace(w.input.Value()); v != "" {
			w.items = append(w.items, v)
			w.input.SetValue("")
			w.cursor = len(w.items)
		}
		return nil
	case "up":
		if w.cursor > 0 {
			w.cursor--
			w.input.Blur()
		}
		return nil
	case "down":
		if w.cursor < len(w.items) {
			w.cursor++
			if w.cursor == len(w.items) {
				return w.input.Focus()
			}
		}
		return nil
	case "delete", "ctrl+d":
		// On an entry these remove it; on the input row they edit the input.
		if w.cursor < len(w.items) {
			w.items = append(w.items[:w.cursor], w.items[w.cursor+1:]...)
			if w.cursor == len(w.items) {
				return w.input.Focus()
			}
			return nil
		}
	}
	if w.cursor != len(w.items) {
		return nil
	}
	var cmd tea.Cmd
	w.input, cmd = w.input.Update(k)
	return cmd
}
func (w *listWidget) Value() string { return strings.Join(w.items, "\n") }
func (w *listWidget) View(focused bool, width int) string {
	var b strings.Builder
	for i, it := range w.items {
		if width > 8 && len(it) > width-4 {
			it = it[:width-7] + "..."
		}
		b.WriteString(cursor(focused && i == w.cursor) + " " + it + "\n")
	}
	b.WriteString(cursor(focused && w.cursor == len(w.items)) + " + " + w.input.View())
	return b.String()
}
func (w *listWidget) Help() string { return "type a key, enter: add • ↑: pick one, delete: remove" }

// ---- helpers ----

func cursor(on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
	}
	return " "
}

// answerFor is the current answer for a field, in the wizard's encoding.
func answerFor(a wizard.Answers, f wizard.Field) string {
	switch f.ID {
	case wizard.FieldDisk:
		return a.Disk
	case wizard.FieldUsername:
		return a.Username
	case wizard.FieldSSHKeys:
		return strings.Join(a.SSHKeys, "\n")
	case wizard.FieldHostname:
		return a.Hostname
	case wizard.FieldTimezone:
		return a.Timezone
	case wizard.FieldKeymap:
		return a.Keymap
	case wizard.FieldFinish:
		return a.FinishAction
	case wizard.FieldExtensions:
		var names []string
		for _, e := range a.Extensions {
			names = append(names, e.Name)
		}
		return strings.Join(names, "\n")
	}
	// A provider gate reads yes when the section it gates was written.
	if section, ok := strings.CutSuffix(f.ID, wizard.AskSuffix); ok {
		return strconv.FormatBool(providerValue(a.Provider, section) != "")
	}
	return providerValue(a.Provider, f.ID)
}

func providerValue(m map[string]any, path string) string {
	keys := strings.Split(path, ".")
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprintf("%v", cur)
}

// visibleWindow returns the half-open range of rows to draw so that cursor
// stays inside it, without scrolling past either end of a list of count rows.
func visibleWindow(cursor, count, rows int) (first, last int) {
	if count <= rows {
		return 0, count
	}
	first = cursor - rows/2
	if first < 0 {
		first = 0
	}
	if first > count-rows {
		first = count - rows
	}
	return first, first + rows
}
