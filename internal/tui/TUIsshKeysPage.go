package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SSH Keys Page
type sshKeysPage struct {
	mode     int // 0 = list view, 1 = add key input
	cursor   int
	sshKeys  []string
	keyInput textinput.Model
}

func newSSHKeysPage() *sshKeysPage {
	keyInput := textinput.New()
	keyInput.Placeholder = "github:USERNAME or gitlab:USERNAME"
	keyInput.Width = 60

	return &sshKeysPage{
		mode:     0,
		cursor:   0,
		sshKeys:  []string{},
		keyInput: keyInput,
	}
}

func (p *sshKeysPage) Init() tea.Cmd {
	return nil
}

func (p *sshKeysPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if p.mode == 0 { // List view
			switch msg.String() {
			case "up", "k":
				if p.cursor > 0 {
					p.cursor--
				}
			case "down", "j":
				if p.cursor < len(p.sshKeys) { // +1 for "Add new key" option
					p.cursor++
				}
			case "d":
				// Delete selected key
				if p.cursor < len(p.sshKeys) {
					p.sshKeys = append(p.sshKeys[:p.cursor], p.sshKeys[p.cursor+1:]...)
					mainModel.sshKeys = append(mainModel.sshKeys[:p.cursor], mainModel.sshKeys[p.cursor+1:]...)
					if p.cursor >= len(p.sshKeys) && p.cursor > 0 {
						p.cursor--
					}
				}
			case "a", "enter":
				if p.cursor == len(p.sshKeys) {
					// Add new key
					p.mode = 1
					p.keyInput.Focus()
					return p, textinput.Blink
				}
			case "esc":
				// Go back to customization page
				return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
			}
		} else { // Add key input mode
			switch msg.String() {
			case "esc":
				p.mode = 0
				p.keyInput.Blur()
				p.keyInput.SetValue("")
				// Go back to customization page
				return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
			case "enter":
				if p.keyInput.Value() != "" {
					p.sshKeys = append(p.sshKeys, p.keyInput.Value())
					mainModel.sshKeys = append(mainModel.sshKeys, p.keyInput.Value())
					p.mode = 0
					p.keyInput.Blur()
					p.keyInput.SetValue("")
					p.cursor = len(p.sshKeys) // Point to "Add new key" option
					return p, textinput.Blink
				}
			}
			p.keyInput, cmd = p.keyInput.Update(msg)
		}
	}

	return p, cmd
}

func (p *sshKeysPage) View() string {
	s := "SSH Keys Management\n\n"

	if p.mode == 0 {
		s += "Current SSH Keys:\n\n"

		for i, key := range p.sshKeys {
			cursor := " "
			if p.cursor == i {
				cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
			}
			// Truncate long keys for display
			displayKey := key
			if len(displayKey) > 50 {
				displayKey = displayKey[:47] + "..."
			}
			s += fmt.Sprintf("%s %s\n", cursor, displayKey)
		}

		// Add "Add new key" option
		cursor := " "
		if p.cursor == len(p.sshKeys) {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		s += fmt.Sprintf("%s + Add new SSH key\n", cursor)

		s += "\nPress 'd' to delete selected key"
	} else {
		s += "Add SSH Public Key:\n\n"
		s += p.keyInput.View() + "\n\n"
		s += "Paste your SSH public key above."
	}

	return s
}

func (p *sshKeysPage) Title() string {
	return "SSH Keys"
}

func (p *sshKeysPage) Help() string {
	if p.mode == 0 {
		return "↑/k: up • ↓/j: down • enter/a: add key • d: delete • esc: back"
	}
	return "Type SSH key • enter: add • esc: cancel"
}

func (p *sshKeysPage) ID() string { return "ssh_keys" }
