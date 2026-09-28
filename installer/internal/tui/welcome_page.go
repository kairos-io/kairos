package tui

import (
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kairos-io/kairos/v4/sdk/agentrun"
	"github.com/kairos-io/kairos/v4/sdk/branding"
	sdkbus "github.com/kairos-io/kairos/v4/sdk/bus"
	qrcode "github.com/skip2/go-qrcode"
)

// welcomePageID is the navigation ID of the welcome page.
const welcomePageID = "welcome"

// welcomePage is the installer's first screen. It says where the web UI this
// same process is already serving can be reached, and renders that address as
// a QR code so it can be opened from a phone without typing it.
//
// Under Advanced it also offers the other remote install the live media has
// always had: the pairing flow, where the agent prints a go-nodepair QR code
// and waits for `kairosctl register` to send a configuration over it.
//
// Until the interactive installer becomes the default live boot, the web UI
// address and the pairing QR code are both produced by the default boot entry
// instead. That entry goes away with the boot flip, which is why the installer
// has to carry them.
//
// With nothing to offer, because the image disabled the web UI, the host has
// no address another machine could open, and no provider is installed to pair
// with, the page auto-advances rather than showing an empty screen, the same
// way the prerequisites page does with no checks.
type welcomePage struct {
	urls []string
	qr   string // QR for urls[0]; empty when there is nothing to encode

	// pairing is whether handing the terminal to `kairos-agent install` would
	// reach the QR code pairing flow. Without a provider to answer the
	// challenge that command has no token to draw and drops to a shell, so
	// offering it then would strand the user.
	pairing bool

	// pairErr is what the pairing handover failed with, kept on screen so a
	// user who pressed "a" and came straight back is told why.
	pairErr string

	loaded bool
}

func newWelcomePage() *welcomePage { return &welcomePage{} }

func (w *welcomePage) ID() string    { return welcomePageID }
func (w *welcomePage) Title() string { return "Welcome" }

func (w *welcomePage) Help() string {
	if w.pairing {
		return "enter: continue • a: pair with a QR code"
	}
	return "enter: continue"
}

// Init reads the web UI's addresses once, renders the QR for the first one,
// and looks for a provider that could drive the pairing install.
//
// A branding file that cannot be read is not an error here: LoadConfig already
// treats an unbranded image as the normal case, and the defaults it returns are
// what an unbranded live ISO actually serves.
func (w *welcomePage) Init() tea.Cmd {
	if !w.loaded {
		cfg, err := branding.LoadConfig()
		if err != nil {
			mainModel.log.Logger.Warn().Err(err).Msg("reading the branding config for the welcome page")
		} else {
			w.urls = cfg.WebUI.URLs()
		}
		if len(w.urls) > 0 {
			w.qr = renderQR(w.urls[0])
		}
		w.pairing = pairingAvailable()
		w.loaded = true
		mainModel.log.Logger.Debug().
			Int("urls", len(w.urls)).
			Bool("qr", w.qr != "").
			Bool("pairing", w.pairing).
			Msg("Welcome page built")
	}

	if len(w.urls) == 0 && !w.pairing {
		return func() tea.Msg { return GoToPageMsg{PageID: "prerequisites"} }
	}
	return nil
}

// pairingFinishedMsg reports that the pairing install the TUI handed the
// terminal to has returned.
type pairingFinishedMsg struct{ err error }

func (w *welcomePage) Update(msg tea.Msg) (Page, tea.Cmd) {
	if done, ok := msg.(pairingFinishedMsg); ok {
		if done.err != nil {
			w.pairErr = done.err.Error()
			return w, nil
		}
		// The agent owned the terminal and has finished, so this machine is
		// installed and, unless its configuration said otherwise, about to
		// reboot. Returning to a welcome page that offers to install it again
		// would be the wrong screen to land on, so the installer is done too.
		return w, tea.Quit
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return w, nil
	}
	switch km.String() {
	case "enter", " ":
		return w, func() tea.Msg { return GoToPageMsg{PageID: "prerequisites"} }
	case "a":
		if w.pairing {
			return w, startPairing(mainModel.source)
		}
	}
	return w, nil
}

func (w *welcomePage) View() string {
	urlStyle := lipgloss.NewStyle().Foreground(kairosAccent).Bold(true)

	lines := make([]string, 0, len(w.urls)+8)
	if len(w.urls) > 0 {
		lines = append(lines, "Install from a browser on another machine", "")
		for _, u := range w.urls {
			lines = append(lines, "  "+urlStyle.Render(u))
		}
	}

	// The advanced section is a few lines and always fits; the QR is 17 and
	// often does not. So the QR is measured against what is left once the
	// advanced section has its room, and it is the first thing to drop: a
	// serial console at 80x24 has no space for it, and a truncated QR is worse
	// than none because it still looks scannable.
	advanced := w.advancedLines(len(lines) > 0)
	if w.qr != "" {
		qrLines := strings.Split(strings.TrimRight(w.qr, "\n"), "\n")
		used := len(lines) + 1 + len(qrLines) + len(advanced) // +1 for the blank line before the QR
		if used <= w.availableLines() {
			lines = append(lines, "")
			lines = append(lines, qrLines...)
		}
	}

	return strings.Join(append(lines, advanced...), "\n") + "\n"
}

// advancedLines renders the Advanced section, which offers the pairing
// install. It is empty when no provider is installed to pair with. separated
// asks for a blank line above it, which is wanted only when something was
// rendered before it.
func (w *welcomePage) advancedLines(separated bool) []string {
	if !w.pairing {
		return nil
	}

	headingStyle := lipgloss.NewStyle().Foreground(kairosHighlight).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(kairosAccent).Bold(true)

	var out []string
	if separated {
		out = append(out, "")
	}
	out = append(out,
		headingStyle.Render("Advanced"),
		"",
		"  "+keyStyle.Render("a")+"  show a QR code and install from \"kairosctl register\"",
	)
	if w.pairErr != "" {
		out = append(out, "", "  "+lipgloss.NewStyle().Foreground(kairosHighlight2).Render("pairing install failed: "+w.pairErr))
	}
	return out
}

// availableLines is how many lines of page content Model.View keeps before it
// truncates, mirroring the budget the disk and prerequisites pages compute.
func (w *welcomePage) availableLines() int {
	_, height := effectiveSize(mainModel.width, mainModel.height)
	return height - visibleRows
}

// pairingAvailable reports whether the pairing install could actually run:
// it needs an agent to hand the terminal to and a provider to answer the
// challenge the agent turns into a QR code.
//
// It is a var so a spec can decide the answer without installing a provider.
var pairingAvailable = func() bool {
	return agentrun.ResolveAgentBin() != "" && sdkbus.HasProviders()
}

// pairingCommand builds the command the TUI suspends itself for. It is a var
// so a spec can drive the handover without running a real kairos-agent.
var pairingCommand = func(source string) *exec.Cmd {
	return agentrun.PairingCommand(agentrun.ResolveAgentBin(), source)
}

// startPairing hands the terminal to the agent's pairing install. bubbletea
// releases the terminal for the duration, which is what lets the agent draw
// its QR code where the user can scan it, and restores the TUI afterwards.
func startPairing(source string) tea.Cmd {
	return tea.ExecProcess(pairingCommand(source), func(err error) tea.Msg {
		return pairingFinishedMsg{err: err}
	})
}

// renderQR encodes s as a terminal-sized QR code, or returns "" when it cannot
// be encoded. A missing QR is not an error: the URL above it is still readable.
func renderQR(s string) string {
	q, err := qrcode.New(s, qrcode.Low)
	if err != nil {
		return ""
	}
	return q.ToSmallString(false)
}
