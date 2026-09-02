package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newTestModel resets the package-global mainModel the pages read from.
func newTestModel() {
	logger := sdkLogger.NewKairosLogger("installer", "fatal", true)
	mainModel = InitialModel(&logger, "")
}

// enter drives one Enter press through the page and runs the command it
// returns, which is where the pages do their navigation work.
func enter(p Page) tea.Msg {
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return nil
	}
	return cmd()
}

// completed returns an install page parked on its last step with no error,
// which is the state the completion screen is rendered from.
func completed() *installProcessPage {
	p := newInstallProcessPage()
	p.progress = len(p.steps) - 1
	return p
}

var _ = Describe("finishAction", func() {
	BeforeEach(newTestModel)

	It("starts at nothing rather than empty", func() {
		Expect(mainModel.finishAction).To(Equal(finishNothing))
	})

	Describe("the install options page", func() {
		It("records the selected action when the install starts", func() {
			p := newInstallOptionsPage()
			p.Update(tea.KeyMsg{Type: tea.KeyRight}) // nothing -> reboot

			Expect(enter(p)).To(Equal(GoToPageMsg{PageID: "summary"}))
			Expect(mainModel.finishAction).To(Equal(finishReboot))
		})

		// The reported bug: this path left finishAction empty, and the
		// completion screen then rendered "System will  shortly".
		It("records the action when the user customizes further instead", func() {
			p := newInstallOptionsPage()
			p.cursor = 1 // Customize Further

			Expect(enter(p)).To(Equal(GoToPageMsg{PageID: "customization"}))
			Expect(mainModel.finishAction).To(Equal(finishNothing))
		})

		It("carries a chosen reboot through the customization path", func() {
			p := newInstallOptionsPage()
			p.Update(tea.KeyMsg{Type: tea.KeyRight}) // nothing -> reboot
			p.cursor = 1                             // Customize Further

			enter(p)
			Expect(mainModel.finishAction).To(Equal(finishReboot))
		})

		It("offers exactly the three actions the agent understands", func() {
			Expect(newInstallOptionsPage().afterInstallOpts).To(Equal(
				[]string{finishNothing, finishReboot, finishPoweroff}))
		})
	})

	Describe("reading the action back", func() {
		It("keeps reboot and poweroff", func() {
			mainModel.finishAction = finishReboot
			Expect(mainModel.finishActionOrNothing()).To(Equal(finishReboot))
			mainModel.finishAction = finishPoweroff
			Expect(mainModel.finishActionOrNothing()).To(Equal(finishPoweroff))
		})

		It("reads an unset or unknown action as nothing", func() {
			mainModel.finishAction = ""
			Expect(mainModel.finishActionOrNothing()).To(Equal(finishNothing))
			mainModel.finishAction = "halt"
			Expect(mainModel.finishActionOrNothing()).To(Equal(finishNothing))
		})
	})

	Describe("the completion screen", func() {
		It("promises the lifecycle action that was selected", func() {
			mainModel.finishAction = finishPoweroff
			Expect(completed().Help()).To(Equal("System will poweroff shortly"))
		})

		It("tells the user how to leave when no action follows", func() {
			mainModel.finishAction = finishNothing
			Expect(completed().Help()).To(Equal("Press any key to exit"))
		})

		// "System will  shortly" is what the user was left staring at.
		It("never promises a blank action", func() {
			mainModel.finishAction = ""
			Expect(completed().Help()).To(Equal("Press any key to exit"))
		})

		It("offers the manual reboot hint when no action follows", func() {
			mainModel.finishAction = ""
			Expect(completed().View()).To(ContainSubstring("You can now reboot or shut down"))
		})

		It("exits on a key press when no action follows", func() {
			mainModel.finishAction = ""
			mainModel.pages = []Page{completed()}
			mainModel.currentPageID = mainModel.pages[0].ID()

			_, cmd := mainModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			Expect(cmd).ToNot(BeNil())
			Expect(cmd()).To(Equal(tea.Quit()))
		})
	})

	Describe("the summary page", func() {
		It("names the action instead of leaving the line blank", func() {
			mainModel.disk = "/dev/sda"
			mainModel.finishAction = ""
			Expect(newSummaryPage().View()).To(ContainSubstring(
				"Action to take when installation is complete: " + finishNothing))
		})
	})

	Describe("the generated cloud config", func() {
		It("sets no lifecycle key for an unset action", func() {
			out, err := RenderCloudConfig(&Model{disk: "/dev/sda"})
			Expect(err).ToNot(HaveOccurred())
			Expect(out).ToNot(ContainSubstring("reboot:"))
			Expect(out).ToNot(ContainSubstring("poweroff:"))
		})
	})
})
