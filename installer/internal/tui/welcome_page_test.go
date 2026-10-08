package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
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

// pairingWelcomePage is a loaded welcome page that also has a provider to pair
// with, so the Advanced section is offered.
func pairingWelcomePage(urls ...string) *welcomePage {
	w := loadedWelcomePage(urls...)
	w.pairing = true
	return w
}

// withPairingCommand runs f with the pairing handover pointed at a harmless
// command, and reports the source it was asked to install from.
func withPairingCommand(f func(), source *string) {
	prev := pairingCommand
	defer func() { pairingCommand = prev }()
	pairingCommand = func(s string) *exec.Cmd {
		*source = s
		return exec.Command("true")
	}
	f()
}

var _ = Describe("welcome page, advanced pairing", func() {
	BeforeEach(func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel.log = &l
	})

	It("offers the pairing install when a provider can answer it", func() {
		w := pairingWelcomePage("http://192.168.1.10:8080")
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("Advanced"))
			Expect(w.View()).To(ContainSubstring("kairosctl register"))
		})
		Expect(w.Help()).To(ContainSubstring("a: pair"))
	})

	It("says nothing about pairing when no provider is installed", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		withTermSize(100, 40, func() {
			Expect(w.View()).NotTo(ContainSubstring("Advanced"))
			Expect(w.View()).NotTo(ContainSubstring("kairosctl register"))
		})
		Expect(w.Help()).NotTo(ContainSubstring("pair"))
	})

	It("hands the terminal to the agent on \"a\", with the installer's source", func() {
		w := pairingWelcomePage("http://192.168.1.10:8080")
		prevSource := mainModel.source
		mainModel.source = "oci:quay.io/kairos/test:latest"
		defer func() { mainModel.source = prevSource }()

		asked := ""
		withPairingCommand(func() {
			_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			Expect(cmd).NotTo(BeNil())
			// tea.ExecProcess returns the program's internal handover message,
			// which is how we know the TUI suspends rather than navigates.
			Expect(fmt.Sprintf("%T", cmd())).To(Equal("tea.execMsg"))
		}, &asked)
		Expect(asked).To(Equal("oci:quay.io/kairos/test:latest"))
	})

	It("ignores \"a\" when there is no provider to pair with", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		Expect(cmd).To(BeNil())
	})

	It("stops the installer once the pairing install has finished", func() {
		w := pairingWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(pairingFinishedMsg{})
		Expect(cmd).NotTo(BeNil())
		Expect(cmd()).To(Equal(tea.Quit()))
	})

	It("stays on the page and says why when the pairing install fails", func() {
		w := pairingWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(pairingFinishedMsg{err: errors.New("exit status 1")})
		Expect(cmd).To(BeNil())
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("exit status 1"))
		})
	})

	It("waits for the user when pairing is the only thing it has to offer", func() {
		w := pairingWelcomePage()
		Expect(w.Init()).To(BeNil())
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("Advanced"))
			Expect(w.View()).NotTo(ContainSubstring("Install from a browser"))
		})
	})

	It("drops the QR before the Advanced section when both do not fit", func() {
		w := pairingWelcomePage("http://192.168.1.10:8080")
		qrFirstLine := strings.Split(strings.TrimRight(w.qr, "\n"), "\n")[0]
		withTermSize(100, 32, func() {
			view := w.View()
			Expect(view).NotTo(ContainSubstring(qrFirstLine))
			Expect(view).To(ContainSubstring("Advanced"))
			Expect(strings.Count(view, "\n")).To(BeNumerically("<=", w.availableLines()))
		})
		// Without the Advanced section the same terminal does have room, so
		// the eviction above is the section's doing and not the QR being too
		// big for the screen either way.
		plain := loadedWelcomePage("http://192.168.1.10:8080")
		withTermSize(100, 32, func() {
			Expect(plain.View()).To(ContainSubstring(qrFirstLine))
		})
	})

	It("keeps the whole page inside the budget Model.View truncates at", func() {
		w := pairingWelcomePage("http://192.168.1.10:8080", "http://10.0.2.15:8080")
		withTermSize(100, 40, func() {
			Expect(strings.Count(w.View(), "\n")).To(BeNumerically("<=", w.availableLines()))
		})
	})
})

// recoveryWelcomePage is a loaded welcome page that also has a provider that
// could answer a recovery challenge, so the Advanced section offers it.
func recoveryWelcomePage(urls ...string) *welcomePage {
	w := loadedWelcomePage(urls...)
	w.recovery = true
	return w
}

// withRecoveryCommand runs f with the recovery handover pointed at a harmless
// command, and reports whether it was asked for.
func withRecoveryCommand(f func(), called *bool) {
	prev := recoveryCommand
	defer func() { recoveryCommand = prev }()
	recoveryCommand = func() *exec.Cmd {
		*called = true
		return exec.Command("true")
	}
	f()
}

var _ = Describe("welcome page, advanced remote recovery", func() {
	BeforeEach(func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel.log = &l
	})

	It("offers remote recovery when a provider can answer it", func() {
		w := recoveryWelcomePage("http://192.168.1.10:8080")
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("Advanced"))
			Expect(w.View()).To(ContainSubstring("kairos bridge"))
		})
		Expect(w.Help()).To(ContainSubstring("r: remote recovery"))
	})

	It("says nothing about recovery when no provider is installed", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		withTermSize(100, 40, func() {
			Expect(w.View()).NotTo(ContainSubstring("kairos bridge"))
		})
		Expect(w.Help()).NotTo(ContainSubstring("recovery"))
	})

	It("hands the terminal to the agent on \"r\"", func() {
		w := recoveryWelcomePage("http://192.168.1.10:8080")
		called := false
		withRecoveryCommand(func() {
			_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			Expect(cmd).NotTo(BeNil())
			// tea.ExecProcess returns the program's internal handover message,
			// which is how we know the TUI suspends rather than navigates.
			Expect(fmt.Sprintf("%T", cmd())).To(Equal("tea.execMsg"))
		}, &called)
		Expect(called).To(BeTrue())
	})

	It("ignores \"r\" when there is no provider to recover through", func() {
		w := loadedWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
		Expect(cmd).To(BeNil())
	})

	It("comes back to the welcome page when recovery is over, rather than quitting", func() {
		w := recoveryWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(recoveryFinishedMsg{})
		Expect(cmd).To(BeNil())
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("Advanced"))
		})
	})

	It("stays on the page and says why when recovery fails", func() {
		w := recoveryWelcomePage("http://192.168.1.10:8080")
		_, cmd := w.Update(recoveryFinishedMsg{err: errors.New("exit status 1")})
		Expect(cmd).To(BeNil())
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("exit status 1"))
		})
	})

	It("clears a previous failure once recovery comes back clean", func() {
		w := recoveryWelcomePage("http://192.168.1.10:8080")
		w.Update(recoveryFinishedMsg{err: errors.New("exit status 1")})
		w.Update(recoveryFinishedMsg{})
		withTermSize(100, 40, func() {
			Expect(w.View()).NotTo(ContainSubstring("exit status 1"))
		})
	})

	It("waits for the user when recovery is the only thing it has to offer", func() {
		w := recoveryWelcomePage()
		Expect(w.Init()).To(BeNil())
		withTermSize(100, 40, func() {
			Expect(w.View()).To(ContainSubstring("kairos bridge"))
			Expect(w.View()).NotTo(ContainSubstring("Install from a browser"))
		})
	})

	It("offers both remote flows under one Advanced heading", func() {
		w := recoveryWelcomePage("http://192.168.1.10:8080")
		w.pairing = true
		withTermSize(100, 40, func() {
			view := w.View()
			Expect(strings.Count(view, "Advanced")).To(Equal(1))
			Expect(view).To(ContainSubstring("kairosctl register"))
			Expect(view).To(ContainSubstring("kairos bridge"))
			Expect(strings.Count(view, "\n")).To(BeNumerically("<=", w.availableLines()))
		})
		Expect(w.Help()).To(ContainSubstring("a: pair"))
		Expect(w.Help()).To(ContainSubstring("r: remote recovery"))
	})
})

var _ = Describe("welcome page, Init wiring", func() {
	BeforeEach(func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel.log = &l
	})

	// withAvailability runs f with both handovers reporting the given answers,
	// so Init is exercised without a provider or an agent on the box.
	withAvailability := func(pair, rec bool, f func()) {
		prevP, prevR := pairingAvailable, recoveryAvailable
		defer func() { pairingAvailable, recoveryAvailable = prevP, prevR }()
		pairingAvailable = func() bool { return pair }
		recoveryAvailable = func() bool { return rec }
		f()
	}

	It("asks recoveryAvailable whether to offer recovery", func() {
		withAvailability(false, true, func() {
			w := newWelcomePage()
			Expect(w.Init()).To(BeNil())
			Expect(w.recovery).To(BeTrue())
			Expect(w.pairing).To(BeFalse())
		})
		withTermSize(100, 40, func() {
			withAvailability(false, true, func() {
				w := newWelcomePage()
				w.Init()
				Expect(w.View()).To(ContainSubstring("kairos bridge"))
			})
		})
	})
})
