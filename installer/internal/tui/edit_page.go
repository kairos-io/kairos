package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// editPageID is the navigation ID of the page that shows and edits the
// generated cloud-config.
const editPageID = "edit_config"

// currentCloudConfig is the configuration the install runs with: what the
// operator saved on the edit page, or what the answers render to, with the
// confirmed disk and finish action written back either way.
func currentCloudConfig() (string, error) {
	text := mainModel.cloudConfig
	if !mainModel.edited {
		rendered, err := wizard.Render(mainModel.answers)
		if err != nil {
			return "", err
		}
		text = rendered
	}
	return wizard.Finalize(text, confirmedOverrides())
}

// confirmedOverrides are the keys the operator confirmed, which the install
// writes back over any text. Saving an edit validates with the same ones.
func confirmedOverrides() wizard.Overrides {
	return wizard.Overrides{
		Device: mainModel.answers.Disk, Source: mainModel.answers.Source, FinishAction: mainModel.answers.FinishAction,
	}
}

// back leaves the page the way the operator came, like a global esc.
func back() tea.Msg { return BackMsg{} }

// editPage shows the cloud-config the install will run with and lets the
// operator change it before the install starts. Under the branding switch
// that hides the optional steps it only shows it.
type editPage struct {
	area textarea.Model
	err  string
	// readOnly is set when wizardEnv.AdvancedDisabled(): any key goes back.
	readOnly bool
	// confirming is set while the page asks whether to throw the edits away.
	confirming bool
}

func newEditPage() *editPage {
	a := textarea.New()
	a.CharLimit = 0
	// MaxHeight also caps the number of lines the text can have.
	a.MaxHeight = 0
	a.ShowLineNumbers = true
	return &editPage{area: a}
}

func (p *editPage) ID() string    { return editPageID }
func (p *editPage) Title() string { return "Configuration" }
func (p *editPage) Help() string {
	if p.readOnly {
		return "any key: back"
	}
	return "ctrl+s: save • ctrl+r: regenerate from answers • esc: back without saving"
}

// resize fits the text area in what the model leaves for a page body.
func (p *editPage) resize() {
	width, height := effectiveSize(mainModel.width, mainModel.height)
	p.area.SetWidth(max(20, width-8))
	p.area.SetHeight(max(3, height-12))
}

// Init reloads the area on every entry, from the saved edit or from the
// answers, so text left with esc never comes back.
func (p *editPage) Init() tea.Cmd {
	p.err, p.confirming = "", false
	p.readOnly = wizardEnv.AdvancedDisabled()
	p.resize()
	if mainModel.edited && !p.readOnly {
		p.area.SetValue(mainModel.cloudConfig)
	} else {
		p.loadGenerated()
	}
	p.area.CursorStart()
	if p.readOnly {
		p.area.Blur()
		return nil
	}
	return p.area.Focus()
}

// loadGenerated puts the configuration rendered from the answers in the area.
func (p *editPage) loadGenerated() {
	text, err := currentCloudConfig()
	if err != nil {
		p.err = "The configuration could not be generated: " + err.Error()
		text = ""
	}
	p.area.SetValue(text)
}

// CapturesKey gives the text area every key, q and ctrl+d included; the
// page handles esc itself.
func (p *editPage) CapturesKey(tea.KeyMsg) bool { return true }

func (p *editPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		p.area, cmd = p.area.Update(msg)
		return p, cmd
	}
	if p.readOnly {
		return p, back
	}
	if p.confirming {
		p.confirming = false
		if k.String() == "y" || k.String() == "Y" {
			mainModel.edited = false
			mainModel.cloudConfig = ""
			p.err = ""
			p.loadGenerated()
		}
		return p, nil
	}
	switch k.String() {
	case "ctrl+s":
		text := p.area.Value()
		if _, err := wizard.Finalize(text, confirmedOverrides()); err != nil {
			p.err = err.Error()
			return p, nil
		}
		mainModel.cloudConfig = text
		mainModel.edited = true
		p.err = ""
		return p, back
	case "ctrl+r":
		p.confirming = true
		return p, nil
	case "esc":
		return p, back
	}
	var cmd tea.Cmd
	p.area, cmd = p.area.Update(k)
	return p, cmd
}

func (p *editPage) View() string {
	warn := lipgloss.NewStyle().Foreground(kairosHighlight2)
	s := "The disk and finish action you chose always win over this text.\n"
	if p.readOnly {
		s = "The configuration the install runs with (read only).\n"
	}
	s += p.area.View() + "\n"
	switch {
	case p.confirming:
		s += warn.Render("Replace your edits with the generated configuration? y/n")
	case p.err != "":
		s += warn.Render(p.err)
	}
	return s
}
