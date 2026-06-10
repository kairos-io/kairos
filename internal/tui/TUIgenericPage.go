package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// genericQuestionPage represents a page that asks a generic question
type genericQuestionPage struct {
	genericInput textinput.Model
	section      YAMLPrompt
}

func (g genericQuestionPage) Init() tea.Cmd {
	return textinput.Blink
}

func (g genericQuestionPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if g.genericInput.Value() == "" && g.section.IfEmpty != "" {
				// If the input is empty and IfEmpty is set, use IfEmpty value
				g.genericInput.SetValue(g.section.IfEmpty)
			}
			// Now if the input is not empty, we can proceed
			if g.genericInput.Value() != "" {
				mainModel.log.Info("Setting value", g.genericInput.Value(), "for section:", g.section.YAMLSection)
				setValueForSectionInMainModel(g.genericInput.Value(), g.section.YAMLSection)
				return g, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
			}
		case "esc":
			// Go back to customization page
			return g, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
		}
	}

	g.genericInput, cmd = g.genericInput.Update(msg)

	return g, cmd
}

func (g genericQuestionPage) View() string {
	s := g.section.Prompt + "\n\n"
	s += g.genericInput.View() + "\n\n"

	return s
}

func (g genericQuestionPage) Title() string {
	return idFromSection(g.section)
}

func (g genericQuestionPage) Help() string {
	return "Press Enter to submit your answer, or esc to cancel."
}

func (g genericQuestionPage) ID() string {
	return idFromSection(g.section)
}

func idFromSection(section YAMLPrompt) string {
	// Generate a unique ID based on the section's YAMLSection.
	// This could be a simple hash or just the section name.
	return strings.Replace(section.YAMLSection, ".", "_", -1)
}

func (g genericQuestionPage) Configured() bool {
	// Check if the section has been configured in mainModel.extraFields
	_, exists := getValueForSectionInMainModel(g.section.YAMLSection)
	return exists
}

// newGenericQuestionPage initializes a new generic question page with a text input Model.
// Uses the provided section to set up the input Model.
func newGenericQuestionPage(section YAMLPrompt) *genericQuestionPage {
	genericInput := textinput.New()
	genericInput.Placeholder = section.PlaceHolder
	genericInput.Width = 120
	genericInput.Focus()

	return &genericQuestionPage{
		genericInput: genericInput,
		section:      section,
	}
}

// genericBoolPage represents a page that asks a generic yes/no question
type genericBoolPage struct {
	cursor  int
	options []string
	section YAMLPrompt
}

func newGenericBoolPage(section YAMLPrompt) *genericBoolPage {
	return &genericBoolPage{
		options: []string{"Yes", "No"},
		cursor:  1, // Default to "No"
		section: section,
	}
}

func (g *genericBoolPage) Title() string {
	return idFromSection(g.section)
}

func (g *genericBoolPage) Help() string {
	return genericNavigationHelp
}

func (g *genericBoolPage) ID() string {
	return idFromSection(g.section)
}

func (g *genericBoolPage) Init() tea.Cmd {
	return nil
}

func (g *genericBoolPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			g.cursor = 0
		case "down", "j":
			g.cursor = 1
		case "enter":
			// in both cases we just go back to customization
			// Save the value to mainModel.extraFields
			mainModel.log.Infof("Setting value %s for section %s:", g.options[g.cursor], g.section.YAMLSection)
			setValueForSectionInMainModel(g.options[g.cursor], g.section.YAMLSection)
			return g, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
		}
	}
	return g, nil
}

func (g *genericBoolPage) View() string {
	s := g.section.Prompt + "\n\n"

	for i, option := range g.options {
		cursor := " "
		if g.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		s += fmt.Sprintf("%s %s\n", cursor, option)
	}

	return s
}

func (g *genericBoolPage) Configured() bool {
	// Check if the section has been configured in mainModel.extraFields
	_, exists := getValueForSectionInMainModel(g.section.YAMLSection)
	return exists
}

// setValueForSectionInMainModel sets a value in the mainModel's extraFields map
// for a given section, which is specified as a dot-separated string.
// It creates nested maps as necessary to reach the specified section.
func setValueForSectionInMainModel(value string, section string) {
	sections := strings.Split(section, ".")
	// Ensure mainModel.extraFields is initialized
	if mainModel.extraFields == nil {
		mainModel.extraFields = make(map[string]interface{})
	}

	currentMap := mainModel.extraFields
	for i, key := range sections {
		if i == len(sections)-1 {
			if value == "Yes" || value == "yes" || value == "true" || value == "True" {
				currentMap[key] = true
			} else if value == "No" || value == "no" || value == "false" || value == "False" {
				currentMap[key] = false
			} else {
				currentMap[key] = value
			}
		} else {
			if nextMap, ok := currentMap[key].(map[string]interface{}); ok {
				currentMap = nextMap
			} else {
				newMap := make(map[string]interface{})
				currentMap[key] = newMap
				currentMap = newMap
			}
		}
	}
}

// getValueForSectionInMainModel retrieves a value from the mainModel's extraFields map
// for a given section, which is specified as a dot-separated string.
func getValueForSectionInMainModel(section string) (string, bool) {
	sections := strings.Split(section, ".")
	currentMap := mainModel.extraFields

	for _, key := range sections {
		if nextMap, ok := currentMap[key].(map[string]interface{}); ok {
			currentMap = nextMap
		} else if value, ok := currentMap[key]; ok {
			return fmt.Sprintf("%v", value), true
		} else {
			return "", false
		}
	}
	return "", false
}
