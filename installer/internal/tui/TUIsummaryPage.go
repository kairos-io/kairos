package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// Summary Page
//
// enter does not start the install: it asks for a y first, because the
// install erases the disk, and the disk step's Confirm warning must be
// acknowledged before that.
type summaryPage struct {
	// confirming is set while the page waits for the y.
	confirming bool
}

func newSummaryPage() *summaryPage {
	return &summaryPage{}
}

func (p *summaryPage) Init() tea.Cmd {
	p.confirming = false
	return nil
}

// CapturesKey keeps q and esc from the model while the page asks for the y,
// so they cancel the question the way any other key does.
func (p *summaryPage) CapturesKey(tea.KeyMsg) bool { return p.confirming }

// diskWarning is the disk step's Confirm text for the chosen disk.
func diskWarning() string {
	step, ok := wizard.StepByID(mainModel.steps, wizard.StepDisk)
	if !ok || len(step.Fields) == 0 || step.Fields[0].Confirm == "" {
		return ""
	}
	return strings.ReplaceAll(step.Fields[0].Confirm, "{value}", mainModel.answers.Disk)
}

func (p *summaryPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	if p.confirming {
		p.confirming = false
		if k.String() == "y" || k.String() == "Y" {
			return p, func() tea.Msg { return GoToPageMsg{PageID: "install_process"} }
		}
		return p, nil
	}
	switch k.String() {
	case "enter":
		p.confirming = true
		return p, nil
	case "e":
		// The branding switch that hides the optional steps hides the
		// editor too; v still shows the configuration.
		if wizardEnv.AdvancedDisabled() {
			return p, nil
		}
		mainModel.viewOnly = false
		return p, func() tea.Msg { return GoToPageMsg{PageID: editPageID} }
	case "v":
		// v only shows the configuration, whether or not e is offered.
		mainModel.viewOnly = true
		return p, func() tea.Msg { return GoToPageMsg{PageID: editPageID} }
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
	if w := diskWarning(); w != "" {
		s += warningStyle.Render(w) + "\n"
	}
	s += "Action to take when installation is complete: " + normalizedFinishAction() + "\n\n"
	if mainModel.quick {
		s += "Quick install: nothing but the disk is configured.\n"
		s += warningStyle.Render("No user was set up, so you cannot log in with a password.") + "\n"
		s += "To add a user, press esc twice and choose Customize.\n"
	} else if !wizardEnv.AdvancedDisabled() {
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
			// A path is a file on the live media, which has no version to
			// resolve; a name is resolved in the catalog.
			if filepath.IsAbs(e.Name) {
				exts = append(exts, filepath.Base(e.Name)+" (live media)")
				continue
			}
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
	if p.confirming {
		s += "\n" + warningStyle.Render("Type y to erase "+a.Disk+" and install, any other key to cancel") + "\n"
	}

	return s
}

func (p *summaryPage) Title() string {
	return "Installation summary"
}

func (p *summaryPage) Help() string {
	if p.confirming {
		return "y: erase the disk and install • any other key: cancel"
	}
	if wizardEnv.AdvancedDisabled() {
		return "enter: start the installation • v: view the configuration"
	}
	return "enter: install • v: view the configuration • e: edit the configuration"
}

func (p *summaryPage) ID() string { return summaryPageID }
