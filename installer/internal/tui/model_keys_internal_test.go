package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// splitLines counts a view's rows the way the model truncates it.
func splitLines(s string) []string { return strings.Split(s, "\n") }

// resolveTimeout is how long resolves waits for one command. It is well past
// what a loaded CI runner needs to schedule the goroutine of a quick command.
const resolveTimeout = 2 * time.Second

// blinkCode is the code of the cursor's blink command, which every focused
// text input and text area returns. It only ever produces a blink, after half
// a second, so resolves skips it instead of waiting for it.
var blinkCode = func() uintptr {
	t := textinput.New()
	return reflect.ValueOf(t.Focus()).Pointer()
}()

// resolves runs cmd, the way bubbletea would, and returns every message it
// produces, unwrapping batches. A cursor blink is not run, and a command still
// blocked after resolveTimeout is given up on: neither can be a quit or a
// navigation.
func resolves(cmd tea.Cmd) []tea.Msg {
	if cmd == nil || reflect.ValueOf(cmd).Pointer() == blinkCode {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, resolves(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(resolveTimeout):
		return nil
	}
}

var _ = Describe("keys the step pages keep from the installer", func() {
	var step *stepPage

	// open lands the model on a step page, reached from the customization
	// menu, so a global esc would have somewhere to go back to.
	open := func(id string) {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		for _, p := range mainModel.pages {
			if sp, ok := p.(*stepPage); ok && sp.ID() == id {
				step = sp
			}
		}
		Expect(step).ToNot(BeNil(), "no step page %q", id)
		mainModel.navigationStack = []string{"customization"}
		mainModel.currentPageID = id
		step.Init()
	}
	press := func(k tea.KeyMsg) []tea.Msg {
		_, cmd := mainModel.Update(k)
		return resolves(cmd)
	}

	It("types a q into a hostname instead of quitting", func() {
		open(wizard.StepHostname)
		msgs := press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		Expect(msgs).ToNot(ContainElement(tea.QuitMsg{}))
		Expect(step.values()[wizard.FieldHostname]).To(Equal("q"))
		Expect(mainModel.currentPageID).To(Equal(wizard.StepHostname))
	})

	It("closes an open choice filter on esc instead of going back", func() {
		open(wizard.StepLocale)
		press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		cw := step.widgets[0].(*choiceWidget)
		Expect(cw.filtering).To(BeTrue())
		press(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(cw.filtering).To(BeFalse())
		Expect(mainModel.currentPageID).To(Equal(wizard.StepLocale))
		Expect(mainModel.navigationStack).To(Equal([]string{"customization"}))
	})

	It("still goes back on esc when no filter is open", func() {
		open(wizard.StepLocale)
		press(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("customization"))
	})

	It("removes a list entry on ctrl+d instead of opening the debug bundle", func() {
		open(wizard.StepSSHKeys)
		mainModel.answers.SSHKeys = []string{"github:a", "github:b"}
		step.Init()
		press(tea.KeyMsg{Type: tea.KeyUp})
		msgs := press(tea.KeyMsg{Type: tea.KeyCtrlD})
		Expect(msgs).ToNot(ContainElement(GoToPageMsg{PageID: DebugBundlePageID}))
		Expect(step.values()[wizard.FieldSSHKeys]).To(Equal("github:a"))
		Expect(mainModel.currentPageID).To(Equal(wizard.StepSSHKeys))
	})

	It("keeps ctrl+d for the debug bundle on a choice list", func() {
		open(wizard.StepDisk)
		msgs := press(tea.KeyMsg{Type: tea.KeyCtrlD})
		Expect(msgs).To(ContainElement(GoToPageMsg{PageID: DebugBundlePageID}))
	})

	It("keeps ctrl+c global even inside a text field", func() {
		open(wizard.StepHostname)
		msgs := press(tea.KeyMsg{Type: tea.KeyCtrlC})
		Expect(msgs).To(ContainElement(tea.QuitMsg{}))
	})

	It("lets the edit page's text area take q and esc", func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		mainModel.answers.Disk = "/dev/vda"
		mainModel.navigationStack = []string{"summary"}
		mainModel.currentPageID = editPageID
		var edit *editPage
		for _, p := range mainModel.pages {
			if e, ok := p.(*editPage); ok {
				edit = e
			}
		}
		Expect(edit).ToNot(BeNil())
		edit.Init()
		msgs := press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		Expect(msgs).ToNot(ContainElement(tea.QuitMsg{}))
		Expect(edit.area.Value()).To(ContainSubstring("q"))
		msgs = press(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(msgs).To(ContainElement(BackMsg{}))
	})
})

var _ = Describe("the shell key", func() {
	var started int

	BeforeEach(func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		started = 0
		prev := shellCommand
		DeferCleanup(func() { shellCommand = prev })
		shellCommand = func() *exec.Cmd {
			started++
			return exec.Command("true")
		}
	})
	press := func(k tea.KeyMsg) tea.Cmd {
		_, cmd := mainModel.Update(k)
		return cmd
	}
	ctrlT := tea.KeyMsg{Type: tea.KeyCtrlT}

	It("hands the terminal to a shell from the welcome page", func() {
		cmd := press(ctrlT)
		Expect(cmd).NotTo(BeNil())
		// tea.ExecProcess returns the program's internal handover message,
		// which is how we know the TUI suspends rather than quits.
		Expect(fmt.Sprintf("%T", cmd())).To(Equal("tea.execMsg"))
		Expect(started).To(Equal(1))
	})

	It("stays on the same page when the shell exits, whatever its status", func() {
		mainModel.navigationStack = []string{welcomePageID}
		mainModel.currentPageID = "prerequisites"
		for _, msg := range []tea.Msg{shellFinishedMsg{}, shellFinishedMsg{err: errors.New("exit status 1")}} {
			_, cmd := mainModel.Update(msg)
			Expect(resolves(cmd)).To(BeEmpty())
			Expect(mainModel.currentPageID).To(Equal("prerequisites"))
			Expect(mainModel.navigationStack).To(Equal([]string{welcomePageID}))
		}
	})

	It("opens a shell from a text field instead of typing into it", func() {
		var step *stepPage
		for _, p := range mainModel.pages {
			if sp, ok := p.(*stepPage); ok && sp.ID() == wizard.StepHostname {
				step = sp
			}
		}
		Expect(step).ToNot(BeNil())
		mainModel.navigationStack = []string{"customization"}
		mainModel.currentPageID = wizard.StepHostname
		step.Init()
		Expect(press(ctrlT)).NotTo(BeNil())
		Expect(started).To(Equal(1))
		Expect(step.values()[wizard.FieldHostname]).To(BeEmpty())
	})

	It("is left to the edit page's text area", func() {
		mainModel.answers.Disk = "/dev/vda"
		mainModel.navigationStack = []string{"summary"}
		mainModel.currentPageID = editPageID
		for _, p := range mainModel.pages {
			if e, ok := p.(*editPage); ok {
				e.Init()
			}
		}
		press(ctrlT)
		Expect(started).To(Equal(0))
	})

	It("is ignored while the install is running", func() {
		mainModel.currentPageID = "install_process"
		press(ctrlT)
		Expect(started).To(Equal(0))
	})

	It("is named in the help line", func() {
		withTermSize(200, 40, func() {
			Expect(mainModel.View()).To(ContainSubstring("ctrl+t: shell"))
		})
	})
})
