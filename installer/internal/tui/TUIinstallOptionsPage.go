package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Install Options Page
type installOptionsPage struct {
	cursor           int
	options          []string
	afterInstallOpts []string
	afterInstallIdx  int
}

func newInstallOptionsPage() *installOptionsPage {
	baseOptions := []string{
		"Start Install",
	}
	// Check if advanced customization is disabled via branding file
	// If the file exists, we do NOT show the "Customize Further" option
	// If the file does not exist, we show the option
	if _, ok := os.Stat(BrandingFile("interactive_install_advanced_disabled")); ok != nil {
		baseOptions = append(baseOptions, "Customize Further (User, SSH Keys, etc.)")
	}
	return &installOptionsPage{
		options:          baseOptions,
		cursor:           0,
		afterInstallOpts: []string{"nothing", "reboot", "poweroff"},
		afterInstallIdx:  0,
	}
}

func (p *installOptionsPage) Init() tea.Cmd {
	return nil
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
			if p.cursor == 0 {
				// Start Install - store after install action in Model.extraFields
				return p, func() tea.Msg {
					// Set the finish action in the main model
					mainModel.finishAction = p.afterInstallOpts[p.afterInstallIdx]
					return GoToPageMsg{PageID: "summary"}
				}
			} else {
				// Customize Further - go to customization page
				return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
			}
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
			for j, val := range p.afterInstallOpts {
				if j == p.afterInstallIdx {
					selector += lipgloss.NewStyle().Bold(true).Foreground(kairosAccent).Render(val)
				} else {
					selector += val
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
