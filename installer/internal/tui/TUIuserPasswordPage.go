package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// User Password Page
type userPasswordPage struct {
	focusedField  int // 0 = username, 1 = password
	usernameInput textinput.Model
	passwordInput textinput.Model
	username      string
	password      string
}

func newUserPasswordPage() *userPasswordPage {
	usernameInput := textinput.New()
	usernameInput.Placeholder = "Kairos"
	usernameInput.Width = 20
	usernameInput.Focus()

	passwordInput := textinput.New()
	passwordInput.Width = 20
	passwordInput.Placeholder = "Kairos"
	passwordInput.EchoMode = textinput.EchoPassword

	return &userPasswordPage{
		focusedField:  0,
		usernameInput: usernameInput,
		passwordInput: passwordInput,
	}
}

func (p *userPasswordPage) Init() tea.Cmd {
	return textinput.Blink
}

func (p *userPasswordPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			if p.focusedField == 0 {
				p.focusedField = 1
				p.usernameInput.Blur()
				p.passwordInput.Focus()
				return p, p.passwordInput.Focus()
			} else {
				p.focusedField = 0
				p.passwordInput.Blur()
				p.usernameInput.Focus()
				return p, p.usernameInput.Focus()
			}
		case "enter":
			if p.usernameInput.Value() != "" && p.passwordInput.Value() != "" {
				p.username = p.usernameInput.Value()
				mainModel.username = p.username
				p.password = p.passwordInput.Value()
				mainModel.password = p.password
				// Save and go back to customization
				return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
			}
		case "esc":
			// Go back to customization page
			return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
		}
	}

	if p.focusedField == 0 {
		p.usernameInput, cmd = p.usernameInput.Update(msg)
	} else {
		p.passwordInput, cmd = p.passwordInput.Update(msg)
	}

	return p, cmd
}

func (p *userPasswordPage) View() string {
	s := "User Account Setup\n\n"
	s += "Username:\n"
	s += p.usernameInput.View() + "\n\n"
	s += "Password:\n"
	s += p.passwordInput.View() + "\n\n"

	if p.username != "" {
		s += fmt.Sprintf("✓ User configured: %s\n", p.username)
	}

	if p.usernameInput.Value() == "" || p.passwordInput.Value() == "" {
		s += "\nBoth fields are required to continue."
	}

	return s
}

func (p *userPasswordPage) Title() string {
	return "User & Password"
}

func (p *userPasswordPage) Help() string {
	return "tab: switch fields • enter: save and continue"
}

func (p *userPasswordPage) ID() string { return "user_password" }
