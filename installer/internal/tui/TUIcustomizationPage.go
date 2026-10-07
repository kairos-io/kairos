package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Customization Page

// customizationPage is the menu of the optional wizard steps, in the order
// wizard.Steps returns them, followed by the way to the summary.
type customizationPage struct {
	cursor  int
	options []string
	// ids holds the page ID each option leads to, by index.
	ids []string
}

func newCustomizationPage() *customizationPage {
	p := &customizationPage{}
	p.load()
	return p
}

// load rebuilds the menu from mainModel.steps.
func (p *customizationPage) load() {
	p.options, p.ids = nil, nil
	for _, s := range mainModel.steps {
		if !s.Optional {
			continue
		}
		p.options = append(p.options, s.Title)
		p.ids = append(p.ids, s.ID)
	}
	p.options = append(p.options, "Finish Customization and start Installation")
	p.ids = append(p.ids, "summary")
	if p.cursor >= len(p.options) {
		p.cursor = 0
	}
}

func (p *customizationPage) Title() string {
	return "Customization"
}

func (p *customizationPage) Help() string {
	return genericNavigationHelp
}

func (p *customizationPage) Init() tea.Cmd {
	p.load()
	return nil
}

func (p *customizationPage) Update(msg tea.Msg) (Page, tea.Cmd) {
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
		case "enter":
			if p.cursor < len(p.ids) {
				pageID := p.ids[p.cursor]
				return p, func() tea.Msg { return GoToPageMsg{PageID: pageID} }
			}
		}
	}
	return p, nil
}

func (p *customizationPage) View() string {
	s := "Customization Options\n\n"
	s += "Configure additional settings:\n\n"

	for i, option := range p.options {
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		tick := ""
		if i < len(p.ids) && p.isConfigured(p.ids[i]) {
			tick = lipgloss.NewStyle().Foreground(kairosAccent).Render(checkMark)
		}
		s += fmt.Sprintf("%s %s %s\n", cursor, option, tick)
	}

	return s
}

func (p *customizationPage) ID() string { return "customization" }

// isConfigured finds the page with this ID and asks it.
func (p *customizationPage) isConfigured(pageID string) bool {
	for _, page := range mainModel.pages {
		if page.ID() != pageID {
			continue
		}
		if c, ok := page.(interface{ Configured() bool }); ok {
			return c.Configured()
		}
	}
	return false
}
