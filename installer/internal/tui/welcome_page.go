package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kairos-io/kairos/v4/sdk/branding"
	qrcode "github.com/skip2/go-qrcode"
)

// welcomePageID is the navigation ID of the welcome page.
const welcomePageID = "welcome"

// welcomePage is the installer's first screen. It says where the web UI this
// same process is already serving can be reached, and renders that address as
// a QR code so it can be opened from a phone without typing it.
//
// Until the interactive installer becomes the default live boot, those two are
// printed by the boot-time console message instead. That message goes away
// with the boot flip, which is why the installer has to carry them.
//
// With nothing to offer, because the image disabled the web UI or the host has
// no address another machine could open, the page auto-advances rather than
// showing an empty screen, the same way the prerequisites page does with no
// checks.
type welcomePage struct {
	urls   []string
	qr     string // QR for urls[0]; empty when there is nothing to encode
	loaded bool
}

// welcomeHeaderLines is the number of lines View renders before the URL list:
// the heading and the blank line under it. It feeds the same height budget the
// disk and prerequisites pages use.
const welcomeHeaderLines = 2

func newWelcomePage() *welcomePage { return &welcomePage{} }

func (w *welcomePage) ID() string    { return welcomePageID }
func (w *welcomePage) Title() string { return "Welcome" }
func (w *welcomePage) Help() string  { return "enter: continue" }

// Init reads the web UI's addresses once and renders the QR for the first one.
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
		w.loaded = true
		mainModel.log.Logger.Debug().Int("urls", len(w.urls)).Bool("qr", w.qr != "").Msg("Welcome page built")
	}

	if len(w.urls) == 0 {
		return func() tea.Msg { return GoToPageMsg{PageID: "prerequisites"} }
	}
	return nil
}

func (w *welcomePage) Update(msg tea.Msg) (Page, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return w, nil
	}
	switch km.String() {
	case "enter", " ":
		return w, func() tea.Msg { return GoToPageMsg{PageID: "prerequisites"} }
	}
	return w, nil
}

func (w *welcomePage) View() string {
	header := "Install from a browser on another machine\n\n"

	urlStyle := lipgloss.NewStyle().Foreground(kairosAccent).Bold(true)
	lines := make([]string, 0, len(w.urls))
	for _, u := range w.urls {
		lines = append(lines, "  "+urlStyle.Render(u))
	}

	// The QR is the first thing to drop when the terminal is short: a serial
	// console at 80x24 has no room for it, and a truncated QR is worse than
	// none because it still looks scannable. The URLs above it are the part
	// that must always survive.
	if w.qr != "" {
		qrLines := strings.Split(strings.TrimRight(w.qr, "\n"), "\n")
		used := welcomeHeaderLines + len(lines) + 1 // +1 for the blank line before the QR
		if used+len(qrLines) <= w.availableLines() {
			lines = append(lines, "")
			lines = append(lines, qrLines...)
		}
	}

	return header + strings.Join(lines, "\n") + "\n"
}

// availableLines is how many lines of page content Model.View keeps before it
// truncates, mirroring the budget the disk and prerequisites pages compute.
func (w *welcomePage) availableLines() int {
	_, height := effectiveSize(mainModel.width, mainModel.height)
	return height - visibleRows
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
