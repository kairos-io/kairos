package tui

import (
	"bytes"

	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

var _ = Describe("choosing how to install", func() {
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	esc := tea.KeyMsg{Type: tea.KeyEsc}
	down := tea.KeyMsg{Type: tea.KeyDown}

	// visited records every page the model shows while the test drives it.
	var visited []string
	step := func(msg tea.Msg) {
		drive(msg)
		visited = append(visited, mainModel.currentPageID)
	}

	// start comes from the prerequisites page, which moved on to the mode
	// page the way it does with no checks.
	start := func(noAdvanced bool) {
		// The install page runs the agent on entry; with none to find it
		// only reports that, so reaching it starts nothing.
		GinkgoT().Setenv("KAIROS_AGENT_BIN", "")
		GinkgoT().Setenv("PATH", GinkgoT().TempDir())
		env := newFakeWizardEnv()
		env.noAdvanced = noAdvanced
		useFakeWizardEnv(env)
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		visited = nil
		mainModel.currentPageID = "prerequisites"
		step(GoToPageMsg{PageID: installModePageID})
	}

	It("puts the mode page between the prerequisites and the disk step", func() {
		start(false)
		Expect(mainModel.currentPageID).To(Equal(installModePageID))
		pre := mainModel.pages[1].(*prerequisitesPage)
		pre.loaded, pre.checks = true, nil
		Expect(pre.advance()()).To(Equal(GoToPageMsg{PageID: installModePageID}))
	})

	It("installs with enter, enter, enter, y, with no user and nothing else configured", func() {
		start(false)
		Expect(mainModel.pages[2].View()).To(ContainSubstring("Quick install"))
		Expect(mainModel.pages[2].View()).To(ContainSubstring("Customize"))

		step(enter)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(mainModel.answers.Disk).To(Equal("/dev/vda"))

		view := newSummaryPage().View()
		Expect(view).To(ContainSubstring("Everything on /dev/vda will be erased."))
		Expect(view).To(ContainSubstring("No user was set up"))
		Expect(view).To(ContainSubstring("Customize"))

		step(enter)
		Expect(mainModel.currentPageID).To(Equal("summary"), "enter asks for the y first")
		step(runes("y"))
		Expect(mainModel.currentPageID).To(Equal("install_process"))

		Expect(visited).ToNot(ContainElement("install_options"))
		Expect(visited).ToNot(ContainElement("customization"))
		Expect(mainModel.answers.FinishAction).To(Equal(""))
		Expect(normalizedFinishAction()).To(Equal("nothing"))
		cc, err := currentCloudConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(cc).To(ContainSubstring("nousers: true"))
		Expect(cc).To(ContainSubstring("device: /dev/vda"))
	})

	It("keeps today's flow behind Customize", func() {
		start(false)
		step(down)
		step(enter)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("install_options"))
		step(down)
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("customization"))
		Expect(newSummaryPage().View()).ToNot(ContainSubstring("No user was set up"))
	})

	It("walks back from the quick summary to the mode page, and Customize keeps the disk", func() {
		start(false)
		step(enter)
		step(down) // the second disk
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(mainModel.answers.Disk).To(Equal("/dev/vdb"))

		step(esc)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		step(esc)
		Expect(mainModel.currentPageID).To(Equal(installModePageID))

		step(down)
		step(enter)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("install_options"))
		Expect(mainModel.answers.Disk).To(Equal("/dev/vdb"))
	})

	It("drops what Customize set when the operator goes back and picks Quick install", func() {
		start(false)
		step(down)
		step(enter)
		step(enter)
		mainModel.answers.Username = "kairos"
		mainModel.answers.Hostname = "edge-01"
		mainModel.answers.FinishAction = wizard.FinishReboot
		step(esc)
		step(esc)
		Expect(mainModel.currentPageID).To(Equal(installModePageID))

		step(tea.KeyMsg{Type: tea.KeyUp})
		step(enter)
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(mainModel.answers.Disk).To(Equal("/dev/vda"))
		Expect(mainModel.answers.Username).To(BeEmpty())
		Expect(mainModel.answers.Hostname).To(BeEmpty())
		Expect(mainModel.answers.FinishAction).To(BeEmpty())
	})

	It("skips the mode page under the branding switch, and esc passes over it", func() {
		start(true)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		Expect(visited).To(Equal([]string{wizard.StepDisk}))
		Expect(mainModel.navigationStack).To(Equal([]string{"prerequisites", installModePageID}))

		// The disk step leads to the install options, where the branding
		// switch still lets the operator pick the finish action.
		step(enter)
		Expect(mainModel.currentPageID).To(Equal("install_options"))
		Expect(newSummaryPage().View()).ToNot(ContainSubstring("No user was set up"))
		step(esc)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))

		// Going back from the disk step does not land on the mode page.
		pre := mainModel.pages[1].(*prerequisitesPage)
		pre.loaded, pre.checks = true, nil
		step(esc)
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
	})
})
