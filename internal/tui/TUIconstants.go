package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
)

var (
	// Default to true color palette
	kairosBg         = lipgloss.Color("#03153a") // Deep blue background
	kairosHighlight  = lipgloss.Color("#e56a44") // Orange highlight
	kairosHighlight2 = lipgloss.Color("#d54b11") // Red-orange highlight
	kairosAccent     = lipgloss.Color("#ee5007") // Accent orange
	kairosBorder     = lipgloss.Color("#e56a44") // Use highlight for border
	kairosText       = lipgloss.Color("#ffffff") // White text for contrast
	checkMark        = "\u2713"
	border           = "normal"
)

func init() {
	// Fallback colors for terminal environments that do not support true color
	term := os.Getenv("TERM")
	if strings.Contains(term, "linux") || strings.Contains(term, "-16color") || term == "dumb" {
		kairosBg = lipgloss.Color("0")         // Black
		kairosText = lipgloss.Color("7")       // White
		kairosHighlight = lipgloss.Color("9")  // Bright Red (for title)
		kairosHighlight2 = lipgloss.Color("1") // Red (for minor alerts or secondary info)
		kairosAccent = lipgloss.Color("5")     // Magenta (or "13" if brighter is OK)
		kairosBorder = lipgloss.Color("9")     // Bright Red (matches highlight)
		checkMark = "*"                        // Use a check mark that works in most terminals
		border = "ascii"
	}
	// Check to see if there is a custom color scheme defined in a file
	brandingFile := BrandingFile("interactive_install_colors")
	if _, err := os.Stat(brandingFile); err == nil {
		f, err := godotenv.Read(brandingFile)
		if err == nil {
			if v, ok := f["KAIROS_BG"]; ok {
				kairosBg = lipgloss.Color(v)
			}
			if v, ok := f["KAIROS_TEXT"]; ok {
				kairosText = lipgloss.Color(v)
			}
			if v, ok := f["KAIROS_HIGHLIGHT"]; ok {
				kairosHighlight = lipgloss.Color(v)
			}
			if v, ok := f["KAIROS_HIGHLIGHT2"]; ok {
				kairosHighlight2 = lipgloss.Color(v)
			}
			if v, ok := f["KAIROS_ACCENT"]; ok {
				kairosAccent = lipgloss.Color(v)
			}
			if v, ok := f["KAIROS_BORDER"]; ok {
				kairosBorder = lipgloss.Color(v)
			}
			if v, ok := f["CHECK_MARK"]; ok {
				checkMark = v
			}
			if v, ok := f["BORDER_STYLE"]; ok {
				border = v
			}
		}
	}

}

const (
	genericNavigationHelp = "↑/k: up • ↓/j: down • enter: select"
	StepPrefix            = "STEP:"
	ErrorPrefix           = "ERROR:"
)

// Installation steps for show
const (
	InstallDefaultStep       = "Preparing installation"
	InstallPartitionStep     = "Partitioning disk"
	InstallBeforeInstallStep = "Running before-install"
	InstallActiveStep        = "Installing active system"
	InstallBootloaderStep    = "Configuring bootloader"
	InstallRecoveryStep      = "Creating recovery image"
	InstallPassiveStep       = "Creating passive image"
	InstallAfterInstallStep  = "Running after-install"
	InstallCompleteStep      = "Installation complete!"
)

// Installation steps to identify installer to UI
const (
	AgentPartitionLog     = "Partitioning device"
	AgentBeforeInstallLog = "Running stage: before-install"
	AgentActiveLog        = "Creating file system image"
	AgentBootloaderLog    = "Installing GRUB"
	AgentRecoveryLog      = "Copying /run/cos/state/cOS/active.img source to /run/cos/recovery/cOS/recovery.img"
	AgentPassiveLog       = "Copying /run/cos/state/cOS/active.img source to /run/cos/state/cOS/passive.img"
	AgentAfterInstallLog  = "Running after-install hook"
	AgentStartLifecycle   = "Running Lifecycle hook"
	AgentCompleteLog      = "Finish Lifecycle hook" // This is the last step before completion so can make it the complete part
)
