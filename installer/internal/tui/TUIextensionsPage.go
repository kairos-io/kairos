package tui

import (
	"context"
	"fmt"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	sdkExtensions "github.com/kairos-io/kairos/v4/sdk/types/extensions"
)

// extensionsPageID is the navigation ID of the extension selection page.
const extensionsPageID = "extensions"

// extensionsLoadedMsg carries the result of the discovery the page starts in
// Init. Discovery reaches the network, so it runs as a command rather than
// inline, and the screen stays responsive while it runs.
type extensionsLoadedMsg struct {
	choices []extensionChoice
	err     error
}

// Extensions Page
type extensionsPage struct {
	cursor   int
	loading  bool
	loadErr  error
	choices  []extensionChoice
	selected map[int]bool

	// catalogs and root are the discovery inputs, overridable by the tests.
	catalogs []string
	root     string
	client   *http.Client
}

func newExtensionsPage() *extensionsPage {
	return &extensionsPage{
		selected: map[int]bool{},
		catalogs: sdkExtensions.Config{}.CatalogURLs(),
		root:     liveMediaDir,
		client:   &http.Client{Timeout: catalogTimeout},
	}
}

func (p *extensionsPage) Init() tea.Cmd {
	// Keep an earlier selection when the user walks back into the page.
	if p.choices != nil || p.loading {
		return nil
	}
	p.loading = true
	root, catalogs, client := p.root, p.catalogs, p.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), catalogTimeout)
		defer cancel()
		choices, err := discoverExtensions(ctx, client, root, catalogs)
		return extensionsLoadedMsg{choices: choices, err: err}
	}
}

func (p *extensionsPage) Update(msg tea.Msg) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case extensionsLoadedMsg:
		p.loading = false
		p.loadErr = msg.err
		p.choices = msg.choices
		if p.choices == nil {
			// Distinguish "discovery ran and found nothing" from "not run
			// yet", so Init does not start a second discovery.
			p.choices = []extensionChoice{}
		}
		mainModel.log.Debugf("Extension discovery returned %d entries", len(p.choices))
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.choices)-1 {
				p.cursor++
			}
		case " ", "x":
			if p.cursor < len(p.choices) {
				p.selected[p.cursor] = !p.selected[p.cursor]
			}
		case "enter":
			mainModel.extensions = selectedExtensions(p.choices, p.selected)
			mainModel.log.Debugf("Selected %d extensions", len(mainModel.extensions))
			return p, func() tea.Msg { return GoToPageMsg{PageID: "customization"} }
		}
	}
	return p, nil
}

func (p *extensionsPage) View() string {
	s := "System Extensions\n\n"
	if p.loading {
		return s + "Looking for extensions on the live media and in the catalog...\n"
	}

	warningStyle := lipgloss.NewStyle().Foreground(kairosHighlight2)
	if p.loadErr != nil {
		s += warningStyle.Render("No catalog could be read, offering only what the live media carries.") + "\n\n"
	}
	if len(p.choices) == 0 {
		return s + "No extension was found on the live media or in the catalog.\n"
	}

	s += "Extensions are merged into the system on the first boot after install.\n\n"

	first, last := visibleWindow(p.cursor, len(p.choices), visibleExtensionRows)
	if first > 0 {
		s += fmt.Sprintf("  ... %d more above\n", first)
	}
	for i := first; i < last; i++ {
		choice := p.choices[i]
		cursor := " "
		if p.cursor == i {
			cursor = lipgloss.NewStyle().Foreground(kairosAccent).Render(">")
		}
		mark := " "
		if p.selected[i] {
			mark = lipgloss.NewStyle().Foreground(kairosAccent).Render(checkMark)
		}
		detail := choice.Origin
		if choice.Latest != "" {
			detail = fmt.Sprintf("%s, latest %s", choice.Origin, choice.Latest)
		}
		s += fmt.Sprintf("%s [%s] %s (%s)\n", cursor, mark, choice.Label, detail)
	}
	if last < len(p.choices) {
		s += fmt.Sprintf("  ... %d more below\n", len(p.choices)-last)
	}
	return s
}

// visibleExtensionRows is how many entries the list shows at once. The model
// truncates a page to the terminal height, and a serial console reports none,
// so the fallback 80x24 leaves about ten lines for the page body. Showing more
// than that would cut the list off with nothing on screen to say so, and the
// cursor could walk off the bottom into rows nobody can see.
const visibleExtensionRows = 7

// visibleWindow returns the half-open range of rows to draw so that cursor
// stays inside it, without scrolling past either end of a list of count rows.
func visibleWindow(cursor, count, rows int) (first, last int) {
	if count <= rows {
		return 0, count
	}
	first = cursor - rows/2
	if first < 0 {
		first = 0
	}
	if first > count-rows {
		first = count - rows
	}
	return first, first + rows
}

func (p *extensionsPage) Title() string {
	return "System Extensions"
}

func (p *extensionsPage) Help() string {
	return "↑/k: up • ↓/j: down • space: select • enter: confirm"
}

func (p *extensionsPage) ID() string { return extensionsPageID }

// Configured reports whether the user picked anything, so the customization
// page can tick the entry.
func (p *extensionsPage) Configured() bool { return len(mainModel.extensions) > 0 }
