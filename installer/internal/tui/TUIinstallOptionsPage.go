package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// Install Options Page
//
// This page asks the wizard's finish step, next to "Start Install", so the
// finish step has no page of its own.
type installOptionsPage struct {
	cursor  int
	options []string
	// afterInstallOpts are the finish step's choices: the label is shown and
	// the value goes through wizard.Apply.
	afterInstallOpts []wizard.Choice
	afterInstallIdx  int
}

func newInstallOptionsPage() *installOptionsPage {
	baseOptions := []string{
		"Start Install",
	}
	// The branding switch hides everything but the disk and the finish action.
	if !wizardEnv.AdvancedDisabled() {
		baseOptions = append(baseOptions, "Customize Further (User, SSH Keys, etc.)")
	}
	p := &installOptionsPage{options: baseOptions}
	if finish, ok := wizard.StepByID(mainModel.steps, wizard.StepFinish); ok && len(finish.Fields) > 0 {
		p.afterInstallOpts = finish.Fields[0].Choices
	}
	p.selectCurrent()
	return p
}

// selectCurrent puts the selector on the finish action already answered,
// or on the field's default.
func (p *installOptionsPage) selectCurrent() {
	p.afterInstallIdx = 0
	for i, c := range p.afterInstallOpts {
		if c.Value == mainModel.answers.FinishAction {
			p.afterInstallIdx = i
		}
	}
}

func (p *installOptionsPage) Init() tea.Cmd {
	p.selectCurrent()
	return nil
}

// applyFinish saves the selected finish action through the wizard.
func (p *installOptionsPage) applyFinish() {
	if p.afterInstallIdx >= len(p.afterInstallOpts) {
		return
	}
	a, errs := wizard.Apply(mainModel.steps, mainModel.answers, wizard.StepFinish,
		map[string]string{wizard.FieldFinish: p.afterInstallOpts[p.afterInstallIdx].Value})
	if len(errs) > 0 {
		mainModel.log.Debugf("finish action not applied: %v", errs)
		return
	}
	mainModel.answers = a
}

func (p *installOptionsPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.options)-1 {
				p.cursor++
			}
		case "left", "h":
			if p.cursor == 0 && p.afterInstallIdx > 0 {
				p.afterInstallIdx--
			}
		case "right", "l":
			if p.cursor == 0 && p.afterInstallIdx < len(p.afterInstallOpts)-1 {
				p.afterInstallIdx++
			}
		case "enter":
			// Save the selected after-install action for either path, so it
			// survives a detour through the customization page.
			p.applyFinish()
			if p.cursor == 0 {
				return p, func() tea.Msg { return GoToPageMsg{PageID: "summary"} }
			}
			return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
		}
	}
	return p, nil
}

func (p *installOptionsPage) View() string {
	s := "Installation Options\n\n"
	s += "Choose how to proceed:\n\n"

	for i, option := range p.options {
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		if i == 0 {
			// Inline selector for Start Install
			selector := "["
			for j, c := range p.afterInstallOpts {
				if j == p.afterInstallIdx {
					selector += lipgloss.NewStyle().Bold(true).Foreground(kairosAccent).Render(c.Label)
				} else {
					selector += c.Label
				}
				if j < len(p.afterInstallOpts)-1 {
					selector += ", "
				}
			}
			selector += "]"
			s += fmt.Sprintf("%s Start Install and on finish do %s\n", cursor, selector)
		} else {
			s += fmt.Sprintf("%s %s\n", cursor, option)
		}
	}

	return s
}

func (p *installOptionsPage) Title() string {
	return "Install Options"
}

func (p *installOptionsPage) Help() string {
	return "↑/k: up • ↓/j: down • enter: select • To select action after install (←/h: left • →/l: right)"
}

func (p *installOptionsPage) ID() string { return "install_options" }
