package tui

import (
	"bytes"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// loadedWelcomePage returns a welcome page that already holds the given URLs,
// skipping Init's read of the host's branding config and interfaces.
func loadedWelcomePage(urls ...string) *welcomePage {
	w := newWelcomePage()
	w.urls = urls
	w.loaded = true
	if len(urls) > 0 {
		w.qr = renderQR(urls[0])
	}
	return w
}

// withTermSize runs f with the model reporting the given terminal size.
func withTermSize(width, height int, f func()) {
	prevW, prevH := mainModel.width, mainModel.height
	mainModel.width, mainModel.height = width, height
	defer func() { mainModel.width, mainModel.height = prevW, prevH }()
	f()
}

var _ = Describe("welcome page", func() {
	BeforeEach(func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel.log = &l
	})

	It("shows every web UI URL", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080", "http://10.0.2.15:8080")
		withTermSize(100, 40, func() {
			view := w.View()
			Expect(view).To(ContainSubstring("192.168.1.10:8080"))
			Expect(view).To(ContainSubstring("10.0.2.15:8080"))
		})
	})

	It("renders the QR for the first URL when the terminal has room", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		Expect(w.qr).NotTo(BeEmpty())
		withTermSize(100, 40, func() {
			view := w.View()
			qrFirstLine := strings.Split(strings.TrimRight(w.qr, "\n"), "\n")[0]
			Expect(view).To(ContainSubstring(qrFirstLine))
		})
	})

	It("drops the QR rather than let Model.View cut it in half", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		qrLines := strings.Split(strings.TrimRight(w.qr, "\n"), "\n")
		withTermSize(80, 24, func() {
			view := w.View()
			Expect(view).To(ContainSubstring("192.168.1.10:8080"))
			Expect(view).NotTo(ContainSubstring(qrLines[0]))
			Expect(strings.Count(view, "\n")).To(BeNumerically("<=", w.availableLines()))
		})
	})

	It("keeps the URLs inside the budget Model.View truncates at", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080", "http://10.0.2.15:8080")
		withTermSize(100, 40, func() {
			Expect(strings.Count(w.View(), "\n")).To(BeNumerically("<=", w.availableLines()))
		})
	})

	It("moves on to the prerequisites page on enter", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(cmd).NotTo(BeNil())
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "prerequisites"}))
	})

	It("stays put on a key that is not a continue", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		Expect(cmd).To(BeNil())
	})

	It("auto-advances when there is no URL to offer", func() {
		w := loadedWelcomePage()
		cmd := w.Init()
		Expect(cmd).NotTo(BeNil())
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "prerequisites"}))
	})

	It("waits for the user when it has something to show", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		Expect(w.Init()).To(BeNil())
	})

	It("is the installer's first page, ahead of the prerequisites page", func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		m := InitialModel(&l, "")
		Expect(m.pages[0].ID()).To(Equal(welcomePageID))
		Expect(m.currentPageID).To(Equal(welcomePageID))
		Expect(m.pages[1].ID()).To(Equal("prerequisites"))
	})
})

var _ = Describe("renderQR", func() {
	It("encodes a URL as a terminal QR", func() {
		Expect(renderQR("http://192.168.1.10:8080")).NotTo(BeEmpty())
	})

	It("returns nothing it cannot encode, rather than failing the screen", func() {
		Expect(renderQR(strings.Repeat("x", 5000))).To(BeEmpty())
	})
})
