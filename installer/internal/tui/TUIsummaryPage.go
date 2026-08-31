package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// Summary Page
type summaryPage struct {
	cursor  int
	options []string
}

func newSummaryPage() *summaryPage {
	return &summaryPage{}
}

func (p *summaryPage) Init() tea.Cmd {
	return nil
}

func (p *summaryPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return p, func() tea.Msg { return GoToPageMsg{PageID: "install_process"} }
		case "v":
			return p, func() tea.Msg { return GoToPageMsg{PageID: "userdata"} }
		}
	}
	return p, nil
}

func (p *summaryPage) View() string {
	warningStyle := lipgloss.NewStyle().Foreground(kairosHighlight2)

	s := "Installation Summary\n\n"
	s += "Selected Disk: " + mainModel.disk + "\n\n"
	s += "Action to take when installation is complete: " + mainModel.finishAction + "\n\n"
	if _, ok := os.Stat(BrandingFile("interactive_install_advanced_disabled")); ok != nil {
		s += "Configuration Summary:\n"
		if mainModel.username != "" {
			s += fmt.Sprintf("  - Username: %s\n", mainModel.username)
		} else {
			s += "  - " + warningStyle.Render("Username: Not set, login to the system wont be possible") + "\n"
		}
		if len(mainModel.sshKeys) > 0 {
			s += fmt.Sprintf("  - SSH Keys: %s\n", mainModel.sshKeys)
		} else {
			s += "  - SSH Keys: Not set\n"
		}

		if len(mainModel.extraFields) > 0 {
			s += "\nExtra Options:\n"
			yamlStr, err := yaml.Marshal(mainModel.extraFields)
			if err == nil {
				s += "\n" + string(yamlStr) + "\n"
			} else {
				s += "    (error displaying extra options)\n"
			}
		} else {
			s += "  - Extra Options: Not set\n"
		}
	}

	return s
}

func (p *summaryPage) Title() string {
	return "Installation summary"
}

func (p *summaryPage) Help() string {
	return "Press enter to start the installation process.\nPress v to view the generated userdata."
}

func (p *summaryPage) ID() string { return "summary" }
