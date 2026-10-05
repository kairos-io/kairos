package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// installModePageID is the navigation ID of the install mode page.
const installModePageID = "install_mode"

// installModeOption is one way to install, as the mode page lists it.
type installModeOption struct {
	label, help string
	quick       bool
}

var installModeOptions = []installModeOption{
	{label: "Quick install", help: "Pick a disk and install. No user is created and nothing else is configured.", quick: true},
	{label: "Customize", help: "Set up a user, SSH keys, hostname, timezone, extensions and more."},
}

// installModePage asks, before the disk step, whether to install with no
// configuration or to go through the customization flow. The choice is
// mainModel.quick, which the disk step reads to pick the page after it.
//
// Under the branding switch that hides the optional steps there is nothing
// to customize, so the page moves on to the disk step by itself and the
// flow is the one that switch has always had: the disk, then the install
// options page with the finish action.
type installModePage struct {
	cursor int
	// chosen is set once the operator picked a mode, so a later visit
	// shows that mode rather than the default.
	chosen bool
}

func newInstallModePage() *installModePage { return &installModePage{} }

func (p *installModePage) ID() string    { return installModePageID }
func (p *installModePage) Title() string { return "Install mode" }
func (p *installModePage) Help() string  { return genericNavigationHelp }

// Skipped reports that the branding switch leaves nothing to choose.
func (p *installModePage) Skipped() bool { return wizardEnv.AdvancedDisabled() }

// Init puts the cursor on the mode already chosen, which is Quick install
// on the first visit.
func (p *installModePage) Init() tea.Cmd {
	if p.Skipped() {
		mainModel.quick = false
		return func() tea.Msg { return GoToPageMsg{PageID: wizard.StepDisk} }
	}
	p.cursor = 0
	if p.chosen && !mainModel.quick {
		p.cursor = 1
	}
	return nil
}

func (p *installModePage) Update(msg tea.Msg) (Page, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch k.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(installModeOptions)-1 {
			p.cursor++
		}
	case "enter":
		quick := installModeOptions[p.cursor].quick
		if quick {
			// A quick install configures nothing, so drop whatever an
			// earlier trip through Customize set or edited. The disk and
			// the source stay: the disk step shows the disk again.
			mainModel.answers = wizard.Answers{Disk: mainModel.answers.Disk, Source: mainModel.answers.Source}
			mainModel.cloudConfig, mainModel.edited = "", false
		}
		mainModel.quick, p.chosen = quick, true
		return p, func() tea.Msg { return GoToPageMsg{PageID: wizard.StepDisk} }
	}
	return p, nil
}

func (p *installModePage) View() string {
	helpStyle := lipgloss.NewStyle().Foreground(kairosText)
	s := "How do you want to install?\n\n"
	for i, o := range installModeOptions {
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		s += fmt.Sprintf("%s %s\n", cursor, o.label)
		s += "    " + helpStyle.Render(o.help) + "\n\n"
	}
	return s
}
