package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

// Page interface that all pages must implement
type Page interface {
	Init() tea.Cmd
	Update(tea.Msg) (Page, tea.Cmd)
	View() string
	Title() string
	Help() string
	ID() string // Unique identifier for the page
}

// NextPageMsg is a custom message type for page navigation
type NextPageMsg struct{}

// GoToPageMsg is a custom message type for navigating to a specific page
// Updated to use PageID instead of PageIndex
type GoToPageMsg struct {
	PageID string
}

// Main application Model
type Model struct {
	pages           []Page
	currentPageID   string   // Track current page by ID
	navigationStack []string // Stack to track navigation history by ID
	width           int
	height          int
	title           string
	disk            string // Selected disk
	username        string
	sshKeys         []string // Store SSH keys
	password        string
	finishAction    string         // Action after installation: reboot, poweroff, none
	extraFields     map[string]any // Dynamic fields for customization
	log             *sdkLogger.KairosLogger
	source          string // cli flags to interactive installer? what??

	showAbortConfirm bool // Show abort confirmation popup
}

var mainModel Model

// InitialModel Initialize the application
func InitialModel(l *sdkLogger.KairosLogger, source string) Model {
	// First create the model with the logger in case any page needs to log something
	mainModel = Model{
		navigationStack: []string{},
		title:           DefaultTitleInteractiveInstaller(),
		source:          source,
		log:             l,
	}
	mainModel.pages = []Page{
		newDiskSelectionPage(),
		newInstallOptionsPage(),
		newCustomizationPage(),
		newUserPasswordPage(),
		newSSHKeysPage(),
		newSummaryPage(),
		newInstallProcessPage(),
		newUserdataPage(),
	}
	mainModel.currentPageID = mainModel.pages[0].ID() // Start with the first page

	mainModel.log.Logger.Debug().Msg("Initial model created")
	return mainModel
}

func (m Model) Init() tea.Cmd {
	mainModel.log.Debug("Starting Kairos Interactive Installer")
	if len(mainModel.pages) > 0 {
		for _, p := range mainModel.pages {
			if p.ID() == mainModel.currentPageID {
				return p.Init()
			}
		}
	}

	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mainModel.log.Tracef("Received message: %T", msg)
	// Deal with window size changes first
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		mainModel.log.Tracef("Window size changed: %dx%d", msg.Width, msg.Height)
		mainModel.width = msg.Width
		mainModel.height = msg.Height
		return m, nil
	}
	// For navigation, access the mainModel so we can modify from anywhere
	currentIdx := -1
	for i, p := range mainModel.pages {
		if p.ID() == mainModel.currentPageID {
			mainModel.log.Tracef("Found current page: %s at index %d", p.ID(), i)
			currentIdx = i
			break
		}
	}
	if currentIdx == -1 {
		mainModel.log.Error("Current page not found in mainModel.pages")
		return mainModel, nil
	}

	// Hijack all keys if on install process page
	if installPage, ok := mainModel.pages[currentIdx].(*installProcessPage); ok {
		// If install failed or finished, any key exits, no abort modal
		if installPage.errorMsg != "" || installPage.progress >= len(installPage.steps)-1 && mainModel.finishAction == "nothing" {
			mainModel.showAbortConfirm = false // Ensure abort modal is closed
			if _, isKey := msg.(tea.KeyMsg); isKey {
				return mainModel, tea.Quit
			}
		}
		// If install finished with no errors and reboot/poweroff selected, block all keys so we dont block the action
		if installPage.errorMsg == "" && installPage.progress >= len(installPage.steps)-1 && (mainModel.finishAction == "reboot" || mainModel.finishAction == "poweroff") {
			return mainModel, nil
		}
		if mainModel.showAbortConfirm {
			// Allow CheckInstallerMsg to update progress even when popup is open
			if _, isCheck := msg.(CheckInstallerMsg); isCheck {
				updatedPage, cmd := installPage.Update(msg)
				mainModel.pages[currentIdx] = updatedPage
				return mainModel, cmd
			}
			// Only handle y/n/esc for popup, block other keys
			if keyMsg, isKey := msg.(tea.KeyMsg); isKey {
				switch keyMsg.String() {
				case "y", "Y":
					installPage.Abort()
					mainModel.showAbortConfirm = false
					fmt.Print("\033[H\033[2J") // Clear the screen before quitting
					return mainModel, tea.Quit
				case "n", "N", "esc":
					mainModel.showAbortConfirm = false
					return mainModel, nil
				}
			}
			// Block all other input
			return mainModel, nil
		}
		if keyMsg, isKey := msg.(tea.KeyMsg); isKey {
			if keyMsg.Type == tea.KeyCtrlC || keyMsg.String() == "ctrl+c" {
				// Only show abort modal if install is not failed/finished
				if installPage.errorMsg == "" && installPage.progress < len(installPage.steps)-1 {
					mainModel.showAbortConfirm = true
					return mainModel, nil
				}
				// If failed/finished, just exit
				return mainModel, tea.Quit
			}
		}
		if installPage.progress < len(installPage.steps)-1 {
			// Ignore all key events during install
			if _, isKey := msg.(tea.KeyMsg); isKey {
				return mainModel, nil
			}
		}
	}
	mainModel.log.Tracef("Dealing with message in mainModel.Update")
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			mainModel.log.Debug("User requested to quit the installer")
			fmt.Print("\033[H\033[2J") // Clear the screen before quitting
			return mainModel, tea.Quit
		case "esc":
			// Go back to previous page if we have navigation history
			if len(mainModel.navigationStack) > 0 {
				// Pop the last page from the stack
				mainModel.currentPageID = mainModel.navigationStack[len(mainModel.navigationStack)-1]
				mainModel.navigationStack = mainModel.navigationStack[:len(mainModel.navigationStack)-1]
				return mainModel, mainModel.pages[currentIdx].Init()
			}
		}
	}

	// Handle page navigation
	if currentIdx < len(mainModel.pages) {
		updatedPage, cmd := mainModel.pages[currentIdx].Update(msg)
		mainModel.pages[currentIdx] = updatedPage

		// Check if we need to navigate to next page
		if _, ok := msg.(NextPageMsg); ok {
			if currentIdx < len(mainModel.pages)-1 {
				// Push current page to navigation stack
				mainModel.navigationStack = append(mainModel.navigationStack, mainModel.currentPageID)
				mainModel.currentPageID = mainModel.pages[currentIdx+1].ID()
				return mainModel, tea.Batch(cmd, mainModel.pages[currentIdx+1].Init())
			}
		}

		// Check if we need to navigate to a specific page
		if goToPageMsg, ok := msg.(GoToPageMsg); ok {
			if goToPageMsg.PageID != "" {
				for i, p := range mainModel.pages {
					if p.ID() == goToPageMsg.PageID {
						mainModel.navigationStack = append(mainModel.navigationStack, mainModel.currentPageID)
						mainModel.currentPageID = goToPageMsg.PageID
						return mainModel, tea.Batch(cmd, mainModel.pages[i].Init())
					}
				}
				mainModel.log.Tracef("model.Update: pageID=%s not found in mainModel.pages", goToPageMsg.PageID)
			}
		}

		return mainModel, cmd
	}

	return mainModel, nil
}

// defaultTermWidth/Height are fallbacks when the terminal doesn't report size (e.g. Alpine, serial console).
const defaultTermWidth, defaultTermHeight = 80, 24

// effectiveSize returns width and height, preferring the model, then env vars (KAIROS_TUI_WIDTH/HEIGHT or COLUMNS/LINES), then defaults.
func effectiveSize(modelW, modelH int) (width, height int) {
	width, height = modelW, modelH
	if width > 0 && height > 0 {
		return width, height
	}
	// Explicit overrides for the TUI (e.g. Alpine where terminal doesn't report size)
	if w := os.Getenv("KAIROS_TUI_WIDTH"); w != "" {
		if n, err := strconv.Atoi(w); err == nil && n > 0 {
			width = n
		}
	}
	if h := os.Getenv("KAIROS_TUI_HEIGHT"); h != "" {
		if n, err := strconv.Atoi(h); err == nil && n > 0 {
			height = n
		}
	}
	// Optional env vars; bash sets them for interactive TTY, other shells often do not (COLUMNS, LINES)
	if width <= 0 {
		if c := os.Getenv("COLUMNS"); c != "" {
			if n, err := strconv.Atoi(c); err == nil && n > 0 {
				width = n
			}
		}
		if width <= 0 {
			width = defaultTermWidth
		}
	}
	if height <= 0 {
		if l := os.Getenv("LINES"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				height = n
			}
		}
		if height <= 0 {
			height = defaultTermHeight
		}
	}
	return width, height
}

func (m Model) View() string {
	mainModel.log.Tracef("Rendering view for current page: %s", mainModel.currentPageID)
	width, height := effectiveSize(mainModel.width, mainModel.height)
	if mainModel.width <= 0 || mainModel.height <= 0 {
		mainModel.log.Tracef("Window size not set (terminal may not report size), using effective %dx%d", width, height)
	}

	var borderStyle lipgloss.Style
	var borderCustomStyle lipgloss.Border
	switch border {
	case "ascii":
		borderCustomStyle = lipgloss.ASCIIBorder()
	case "rounded":
		borderCustomStyle = lipgloss.RoundedBorder()
	case "double":
		borderCustomStyle = lipgloss.DoubleBorder()
	case "thick":
		borderCustomStyle = lipgloss.ThickBorder()
	default:
		borderCustomStyle = lipgloss.NormalBorder()
	}

	// Check if its disabled
	if border == "none" || border == "disabled" || border == "off" || border == "false" {
		borderStyle = lipgloss.NewStyle().
			Background(kairosBg).
			Padding(2).
			Width(width).
			Height(height)
	} else {
		borderStyle = lipgloss.NewStyle().
			Border(borderCustomStyle).
			BorderForeground(kairosBorder, kairosBorder, kairosBorder, kairosBorder).
			Background(kairosBg).
			Padding(1).
			Width(width - 4).
			Height(height - 4)
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(kairosHighlight).
		Background(kairosBg).
		Padding(0, 0).
		Width(width - 6). // Set width to match content area
		Align(lipgloss.Center)

	// Get current page content by ID
	content := ""
	help := ""
	for _, p := range mainModel.pages {
		if p.ID() == mainModel.currentPageID {
			content = p.View()
			help = p.Help()
			break
		}
	}

	title := titleStyle.Render(mainModel.title)

	helpStyle := lipgloss.NewStyle().
		Foreground(kairosText).
		Italic(true)

	var fullHelp string
	currentIdx := -1
	for i, p := range mainModel.pages {
		if p.ID() == mainModel.currentPageID {
			currentIdx = i
			break
		}
	}
	if currentIdx != -1 {
		if _, ok := mainModel.pages[currentIdx].(*installProcessPage); ok {
			fullHelp = help
		} else if _, ok := mainModel.pages[currentIdx].(*summaryPage); ok {
			fullHelp = help
		} else if _, ok := mainModel.pages[currentIdx].(*userdataPage); ok {
			fullHelp = help
		} else {
			fullHelp = help + " • ESC: back • q/ctrl+c: quit"
		}
	}

	helpText := helpStyle.Render(fullHelp)

	availableHeight := height - 8
	contentHeight := availableHeight - 2
	contentLines := strings.Split(content, "\n")
	if len(contentLines) > contentHeight {
		contentLines = contentLines[:contentHeight]
		content = strings.Join(contentLines, "\n")
	}

	pageContent := fmt.Sprintf("%s\n\n%s\n\n%s", title, content, helpText)

	if mainModel.showAbortConfirm {
		mainModel.log.Debug("Showing abort confirmation popup")
		popupStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(kairosAccent).
			Background(kairosBg).
			Padding(1, 2).
			Align(lipgloss.Center)
		popupMsg := "Are you sure you want to abort the installation? (y/n)"
		popup := popupStyle.Render(popupMsg)
		// Overlay the popup in the center
		return fmt.Sprintf("%s\n\n%s", borderStyle.Render(pageContent), lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup))
	}

	return borderStyle.Render(pageContent)
}
