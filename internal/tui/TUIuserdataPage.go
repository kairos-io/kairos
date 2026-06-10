package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Userdata Page

type userdataPage struct{}

func (p *userdataPage) Title() string {
	return "Userdata Generated"
}

func (p *userdataPage) Help() string {
	return "Press any key to return to the summary page."
}

func (p *userdataPage) ID() string {
	return "userdata"
}

func newUserdataPage() *userdataPage {
	return &userdataPage{}
}

func (p *userdataPage) Init() tea.Cmd {
	return nil
}

func (p *userdataPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		// Go back to customization page
		return p, func() tea.Msg { return GoToPageMsg{PageID: "summary"} }
	}
	return p, nil
}

func (p *userdataPage) View() string {
	s := "Userdata Generated (plain text):\n"
	ccString, err := RenderCloudConfig(&mainModel)
	if err == nil {
		s += "\n" + ccString + "\n"
	} else {
		s += " (error displaying cloud config)\n"
	}
	return s
}
