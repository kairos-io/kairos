package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	sdk "github.com/kairos-io/kairos-sdk/bus"
	"github.com/mudler/go-pluggable"
)

// Customization Page

// providerBus discovers and talks to agent-provider-* plugins. The kairos-sdk
// bus already provides the manager (autoload prefix/paths, Initialize), so we
// don't keep a local copy.
var providerBus = sdk.NewBus()

// Discover and run plugins for customization
func runCustomizationPlugins() ([]sdk.YAMLPrompt, error) {
	providerBus.Initialize()
	var r []sdk.YAMLPrompt

	providerBus.Response(sdk.EventInteractiveInstall, func(p *pluggable.Plugin, resp *pluggable.EventResponse) {
		if resp.Data == "" {
			return
		}
		if err := json.Unmarshal([]byte(resp.Data), &r); err != nil {
			fmt.Println(err)
		}
	})

	_, err := providerBus.Publish(sdk.EventInteractiveInstall, sdk.EventPayload{})
	if err != nil {
		return r, err
	}

	return r, nil

}

func newCustomizationPage() *customizationPage {
	return &customizationPage{
		options: []string{
			"User & Password",
			"SSH Keys",
		},

		cursor: 0,
		cursorWithIds: map[int]string{
			0: "user_password",
			1: "ssh_keys",
		},
	}
}

func checkPageExists(pageID string, options map[int]string) bool {
	for _, opt := range options {
		if strings.Contains(opt, pageID) {
			return true
		}
	}
	return false
}

type customizationPage struct {
	cursor        int
	options       []string
	cursorWithIds map[int]string
}

func (p *customizationPage) Title() string {
	return "Customization"
}

func (p *customizationPage) Help() string {
	return genericNavigationHelp
}

func (p *customizationPage) Init() tea.Cmd {
	mainModel.log.Debugf("Running customization plugins...")
	yaML, err := runCustomizationPlugins()
	if err != nil {
		mainModel.log.Debugf("Error running customization plugins: %v", err)
		return nil
	}
	if len(yaML) > 0 {
		startIdx := len(p.options)
		for i, prompt := range yaML {
			// Check if its already added to the options!
			if checkPageExists(idFromSection(prompt), p.cursorWithIds) {
				mainModel.log.Debugf("Customization page for %s already exists, skipping", prompt.YAMLSection)
				continue
			}
			optIdx := startIdx + i
			if prompt.Bool == false {
				mainModel.log.Debugf("Adding customization option for %s", prompt.YAMLSection)
				p.options = append(p.options, fmt.Sprintf("Configure %s", prompt.YAMLSection))
				pageID := idFromSection(prompt)
				p.cursorWithIds[optIdx] = pageID
				newPage := newGenericQuestionPage(prompt)
				mainModel.pages = append(mainModel.pages, newPage)
			} else {
				mainModel.log.Debugf("Adding customization option(bool) for %s", prompt.YAMLSection)
				p.options = append(p.options, fmt.Sprintf("Configure %s", prompt.YAMLSection))
				pageID := idFromSection(prompt)
				p.cursorWithIds[optIdx] = pageID
				newPage := newGenericBoolPage(prompt)
				mainModel.pages = append(mainModel.pages, newPage)
			}
		}
	}

	// Now add the finish and install options to the bottom of the list
	if !checkPageExists("summary", p.cursorWithIds) {
		p.options = append(p.options, "Finish Customization and start Installation")
		p.cursorWithIds[len(p.cursorWithIds)] = "summary"
	}

	mainModel.log.Debugf("Customization options loaded: %v", p.cursorWithIds)
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
			if pageID, ok := p.cursorWithIds[p.cursor]; ok {
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
		pageID, ok := p.cursorWithIds[i]
		if ok && p.isConfigured(pageID) {
			tick = lipgloss.NewStyle().Foreground(kairosAccent).Render(checkMark)
		}
		s += fmt.Sprintf("%s %s %s\n", cursor, option, tick)
	}

	return s
}

// Helper methods to check configuration
func (p *customizationPage) isUserConfigured() bool {
	return mainModel.username != "" && mainModel.password != ""
}

func (p *customizationPage) isSSHConfigured() bool {
	return len(mainModel.sshKeys) > 0
}

func (p *customizationPage) ID() string { return "customization" }

// isConfigured checks if a given pageID is configured, supporting both static and dynamic fields
func (p *customizationPage) isConfigured(pageID string) bool {
	// Hardcoded checks for static fields
	if pageID == "user_password" {
		return p.isUserConfigured()
	}
	if pageID == "ssh_keys" {
		return p.isSSHConfigured()
	}
	// Try to find a page with this ID and call Configured() if available
	for _, page := range mainModel.pages {
		if idProvider, ok := page.(interface{ ID() string }); ok && idProvider.ID() == pageID {
			// We found the page with the given ID, check if it has a Configured method
			if configuredProvider, ok := page.(interface{ Configured() bool }); ok {
				// Call the Configured method to check if it's configured
				return configuredProvider.Configured()
			}
		}
	}
	return false
}
