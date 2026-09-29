package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

var _ = Describe("the generated configuration in the terminal installer", func() {
	BeforeEach(func() {
		l := sdkLogger.NewKairosLogger("test", "error", false)
		mainModel = Model{log: &l, answers: wizard.Answers{Disk: "/dev/vda", Hostname: "edge-01", FinishAction: wizard.FinishReboot}}
	})

	It("installs what Render produced when nothing was edited", func() {
		out, err := currentCloudConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("hostname: edge-01"))
		Expect(out).To(ContainSubstring("device: /dev/vda"))
	})

	It("installs the edited text, with the confirmed disk and finish action written back", func() {
		p := newEditPage()
		p.Init()
		p.area.SetValue("#cloud-config\ninstall:\n  device: /dev/vdb\n  poweroff: true\nk3s:\n  enabled: true\n")
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(cmd).ToNot(BeNil())
		Expect(mainModel.edited).To(BeTrue())
		out, err := currentCloudConfig()
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("device: /dev/vda"))
		Expect(out).To(ContainSubstring("reboot: true"))
		Expect(out).To(ContainSubstring("poweroff: false"))
		Expect(out).To(ContainSubstring("k3s:"))
	})

	It("refuses to save text that is not YAML and says why", func() {
		p := newEditPage()
		p.Init()
		p.area.SetValue("#cloud-config\nusers: [oops\n")
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(cmd).To(BeNil())
		Expect(p.View()).To(ContainSubstring("not valid YAML"))
		Expect(mainModel.edited).To(BeFalse())
	})

	It("lists every optional step on the customization menu, in order", func() {
		mainModel.steps = []wizard.Step{{ID: wizard.StepDisk}, {ID: wizard.StepUser, Title: "User and password", Optional: true}, {ID: wizard.StepHostname, Title: "Hostname", Optional: true}, {ID: wizard.StepFinish}}
		p := newCustomizationPage()
		p.Init()
		Expect(p.options).To(Equal([]string{"User and password", "Hostname", "Finish Customization and start Installation"}))
	})

	// Beyond the brief's four: the page's own keys.
	It("goes back to the summary on esc without saving", func() {
		p := newEditPage()
		p.Init()
		p.area.SetValue("#cloud-config\nk3s: {}\n")
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(cmd).ToNot(BeNil())
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "summary"}))
		Expect(mainModel.edited).To(BeFalse())
	})

	It("replaces the edits with the generated configuration only after y", func() {
		p := newEditPage()
		p.Init()
		p.area.SetValue("#cloud-config\nk3s:\n  enabled: true\n")
		p.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(mainModel.edited).To(BeTrue())

		p.Init()
		Expect(p.area.Value()).To(ContainSubstring("k3s:"), "a saved edit is what the page reopens on")
		p.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
		Expect(p.View()).To(ContainSubstring("Replace your edits with the generated configuration? y/n"))
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		Expect(mainModel.edited).To(BeTrue())
		Expect(p.area.Value()).To(ContainSubstring("k3s:"))

		p.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		Expect(mainModel.edited).To(BeFalse())
		Expect(p.area.Value()).To(ContainSubstring("hostname: edge-01"))
		Expect(p.area.Value()).ToNot(ContainSubstring("k3s:"))
	})

	It("fits an 80x24 console", func() {
		p := newEditPage()
		p.Init()
		budget := defaultTermHeight - 8 - 2
		Expect(len(splitLines(p.View()))).To(BeNumerically("<=", budget))
	})
})
