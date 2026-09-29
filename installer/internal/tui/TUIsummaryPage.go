package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Summary Page
type summaryPage struct{}

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
		case "e", "v":
			return p, func() tea.Msg { return GoToPageMsg{PageID: editPageID} }
		}
	}
	return p, nil
}

// orNotSet is v, or "Not set" when v is empty.
func orNotSet(v string) string {
	if v == "" {
		return "Not set"
	}
	return v
}

func (p *summaryPage) View() string {
	warningStyle := lipgloss.NewStyle().Foreground(kairosHighlight2)
	a := mainModel.answers

	s := "Installation Summary\n\n"
	s += "Selected Disk: " + a.Disk + "\n"
	s += "Action to take when installation is complete: " + normalizedFinishAction() + "\n\n"
	if !wizardEnv.AdvancedDisabled() {
		s += "Configuration Summary:\n"
		if a.Username != "" {
			s += fmt.Sprintf("  - Username: %s\n", a.Username)
		} else {
			s += "  - " + warningStyle.Render("Username: Not set, login to the system wont be possible") + "\n"
		}
		if len(a.SSHKeys) > 0 {
			keys := strings.Join(a.SSHKeys, ", ")
			if width, _ := effectiveSize(mainModel.width, mainModel.height); len(keys) > width-30 && width > 40 {
				keys = keys[:width-33] + "..."
			}
			s += fmt.Sprintf("  - SSH Keys: %d (%s)\n", len(a.SSHKeys), keys)
		} else {
			s += "  - SSH Keys: Not set\n"
		}
		s += "  - Hostname: " + orNotSet(a.Hostname) + "\n"
		s += "  - Timezone: " + orNotSet(a.Timezone) + ", keyboard: " + orNotSet(a.Keymap) + "\n"

		var exts []string
		for _, e := range a.Extensions {
			version := e.Version
			if version == "" {
				version = "latest"
			}
			exts = append(exts, fmt.Sprintf("%s (%s)", e.Name, version))
		}
		s += "  - System Extensions: " + orNotSet(strings.Join(exts, ", ")) + "\n"

		var provider []string
		for k := range a.Provider {
			provider = append(provider, k)
		}
		sort.Strings(provider)
		s += "  - Provider settings: " + orNotSet(strings.Join(provider, ", ")) + "\n"
	}
	if mainModel.edited {
		s += "\n" + warningStyle.Render("Edited by hand: only the disk and finish action above override the edited text.") + "\n"
	}

	return s
}

func (p *summaryPage) Title() string {
	return "Installation summary"
}

func (p *summaryPage) Help() string {
	return "enter: start the installation • e: view and edit the configuration"
}

func (p *summaryPage) ID() string { return "summary" }
