package tui

import (
	"bytes"

	tea "github.com/charmbracelet/bubbletea"
	sdkExtensions "github.com/kairos-io/kairos/v4/sdk/types/extensions"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// drive sends msg to the model the way bubbletea would, and feeds every
// message its command produces back in, so navigation happens for real.
func drive(msg tea.Msg) {
	driveDepth(msg, 0)
}

func driveDepth(msg tea.Msg, depth int) {
	_, cmd := mainModel.Update(msg)
	if depth > 8 {
		return
	}
	for _, m := range resolves(cmd) {
		if _, quit := m.(tea.QuitMsg); quit {
			continue
		}
		driveDepth(m, depth+1)
	}
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func findEditPage() *editPage {
	for _, p := range mainModel.pages {
		if e, ok := p.(*editPage); ok {
			return e
		}
	}
	Fail("no edit page")
	return nil
}

var _ = Describe("walking through the installer", func() {
	// start lands on the install options page, reached from the disk step.
	start := func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		mainModel.answers.Disk = "/dev/vda"
		mainModel.navigationStack = []string{wizard.StepDisk}
		mainModel.currentPageID = "install_options"
	}

	It("leaves the editor on esc the way the operator came, and forgets the discarded text", func() {
		start()
		drive(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(mainModel.currentPageID).To(Equal("summary"))
		drive(runes("e"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
		drive(runes("zzz"))
		Expect(findEditPage().area.Value()).To(ContainSubstring("zzz"))

		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(mainModel.edited).To(BeFalse())
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("install_options"))
		Expect(mainModel.navigationStack).To(Equal([]string{wizard.StepDisk}))

		drive(tea.KeyMsg{Type: tea.KeyEnter})
		drive(runes("e"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
		Expect(findEditPage().area.Value()).ToNot(ContainSubstring("zzz"))
		Expect(findEditPage().area.Value()).To(ContainSubstring("device: /dev/vda"))
	})

	It("returns to the summary after a save, and esc from there does not reopen the editor", func() {
		start()
		drive(tea.KeyMsg{Type: tea.KeyEnter})
		drive(runes("e"))
		findEditPage().area.SetValue("#cloud-config\nk3s:\n  enabled: true\n")
		drive(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(mainModel.edited).To(BeTrue())
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("install_options"))
	})

	It("re-scans the disks when esc walks back to the disk step (#4260)", func() {
		env := newFakeWizardEnv()
		useFakeWizardEnv(env)
		start()
		before := *env.diskCalls
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		Expect(*env.diskCalls).To(Equal(before + 1))
	})

	It("reloads a step's saved answer when esc walks back to it", func() {
		start()
		mainModel.answers.Hostname = "edge-01"
		mainModel.navigationStack = []string{"customization", wizard.StepHostname}
		mainModel.currentPageID = "customization"
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal(wizard.StepHostname))
		for _, p := range mainModel.pages {
			if sp, ok := p.(*stepPage); ok && sp.ID() == wizard.StepHostname {
				Expect(sp.values()[wizard.FieldHostname]).To(Equal("edge-01"))
			}
		}
	})

	It("does not walk back into pages that had nothing to show", func() {
		start()
		// The welcome and prerequisites pages skipped themselves on the way
		// in. Set that state directly: their Init reads the host's branding,
		// providers and checks.
		mainModel.pages[0].(*welcomePage).loaded = true
		pre := mainModel.pages[1].(*prerequisitesPage)
		pre.loaded, pre.checks = true, nil
		mainModel.navigationStack = []string{welcomePageID, "prerequisites"}
		mainModel.currentPageID = wizard.StepDisk
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal(wizard.StepDisk))
		Expect(mainModel.navigationStack).To(Equal([]string{welcomePageID, "prerequisites"}))

		// With something on the welcome page, esc reaches it rather than
		// bouncing off the empty prerequisites page back to the disk step.
		mainModel.pages[0].(*welcomePage).urls = []string{"http://192.168.1.10:8080"}
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal(welcomePageID))
		Expect(mainModel.navigationStack).To(BeEmpty())
	})
})

var _ = Describe("the configuration page under the branding switch", func() {
	open := func(noAdvanced bool) {
		env := newFakeWizardEnv()
		env.noAdvanced = noAdvanced
		useFakeWizardEnv(env)
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		mainModel.answers.Disk = "/dev/vda"
		mainModel.navigationStack = []string{"install_options"}
		mainModel.currentPageID = "summary"
	}

	It("offers editing when advanced options are on", func() {
		open(false)
		Expect(newSummaryPage().Help()).To(ContainSubstring("e: edit the configuration"))
		drive(runes("e"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
		drive(runes("x"))
		Expect(findEditPage().area.Value()).To(ContainSubstring("x"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
	})

	It("keeps v a read-only view when advanced options are on, and e the editor", func() {
		open(false)
		help := newSummaryPage().Help()
		Expect(help).To(ContainSubstring("v: view the configuration"))
		Expect(help).To(ContainSubstring("e: edit the configuration"))

		drive(runes("v"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
		edit := findEditPage()
		Expect(edit.readOnly).To(BeTrue())
		Expect(edit.Help()).ToNot(ContainSubstring("ctrl+s"))
		generated := edit.area.Value()
		Expect(generated).To(ContainSubstring("device: /dev/vda"))

		// Typing does not change it: any key leaves.
		drive(runes("x"))
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(edit.area.Value()).To(Equal(generated))

		// ctrl+s does not save.
		drive(runes("v"))
		drive(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(mainModel.edited).To(BeFalse())
		Expect(mainModel.currentPageID).To(Equal("summary"))

		// e, after v, opens the page editable.
		drive(runes("e"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
		Expect(edit.readOnly).To(BeFalse())
		drive(runes("x"))
		Expect(edit.area.Value()).To(ContainSubstring("x"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
	})

	It("shows a saved edit on v, the text the install will run with", func() {
		open(false)
		drive(runes("e"))
		findEditPage().area.SetValue("#cloud-config\nk3s:\n  enabled: true\n")
		drive(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(mainModel.edited).To(BeTrue())
		drive(runes("v"))
		Expect(findEditPage().readOnly).To(BeTrue())
		Expect(findEditPage().area.Value()).To(ContainSubstring("k3s:"))
		Expect(findEditPage().area.Value()).To(ContainSubstring("device: /dev/vda"))
	})

	It("only shows the configuration when advanced options are off", func() {
		open(true)
		help := newSummaryPage().Help()
		Expect(help).ToNot(ContainSubstring("edit"))
		Expect(help).To(ContainSubstring("v: view the configuration"))

		drive(runes("e"))
		Expect(mainModel.currentPageID).To(Equal("summary"), "e must not open an editor")

		drive(runes("v"))
		Expect(mainModel.currentPageID).To(Equal(editPageID))
		edit := findEditPage()
		Expect(edit.View()).To(ContainSubstring("device: /dev/vda"))
		Expect(edit.Help()).ToNot(ContainSubstring("ctrl+s"))
		generated := edit.area.Value()

		// ctrl+s and ctrl+r do nothing but leave, like any other key.
		drive(tea.KeyMsg{Type: tea.KeyCtrlS})
		Expect(mainModel.edited).To(BeFalse())
		Expect(mainModel.currentPageID).To(Equal("summary"))

		drive(runes("v"))
		drive(runes("x"))
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(edit.area.Value()).To(Equal(generated))

		drive(runes("v"))
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("summary"))
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("install_options"))
	})
})

var _ = Describe("the configuration a debug bundle carries", func() {
	BeforeEach(func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = Model{log: &l, answers: wizard.Answers{Disk: "/dev/vda", Username: "kairos", PasswordHash: "$6$secret"}}
	})

	It("is the install's configuration with the password redacted", func() {
		out := bundledCloudConfig()
		Expect(out).To(ContainSubstring("device: /dev/vda"))
		Expect(out).ToNot(ContainSubstring("$6$secret"))
	})

	It("is still a redacted placeholder when the configuration cannot be produced", func() {
		mainModel.edited = true
		mainModel.cloudConfig = "#cloud-config\ninstall: [not, a, mapping]\npasswd: hunter2\n"
		out := bundledCloudConfig()
		Expect(out).To(HavePrefix("#cloud-config\n"))
		Expect(out).To(ContainSubstring(wizard.Redacted))
		Expect(out).ToNot(ContainSubstring("hunter2"))
	})
})

var _ = Describe("the keys the operator confirmed", func() {
	It("are the same for the install and for saving an edit", func() {
		mainModel = Model{answers: wizard.Answers{Disk: "/dev/vda", Source: "oci:example/image:tag", FinishAction: wizard.FinishPoweroff}}
		Expect(confirmedOverrides()).To(Equal(wizard.Overrides{Device: "/dev/vda", Source: "oci:example/image:tag", FinishAction: wizard.FinishPoweroff}))
	})
})

var _ = Describe("starting the install from the summary", func() {
	BeforeEach(func() {
		// The install page runs the agent on entry; with none to find it
		// only reports that, so reaching it starts nothing.
		GinkgoT().Setenv("KAIROS_AGENT_BIN", "")
		GinkgoT().Setenv("PATH", GinkgoT().TempDir())
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		mainModel.answers.Disk = "/dev/vda"
		mainModel.navigationStack = []string{"install_options"}
		mainModel.currentPageID = "summary"
	})

	summary := func() *summaryPage {
		for _, p := range mainModel.pages {
			if s, ok := p.(*summaryPage); ok {
				return s
			}
		}
		Fail("no summary page")
		return nil
	}

	It("shows the disk step's warning with the chosen disk", func() {
		Expect(summary().View()).To(ContainSubstring("Everything on /dev/vda will be erased."))
	})

	It("asks before erasing the disk, and any key but y cancels", func() {
		drive(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(summary().View()).To(ContainSubstring("Type y to erase /dev/vda and install, any other key to cancel"))
		drive(runes("n"))
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(summary().View()).ToNot(ContainSubstring("Type y to erase"))

		// q and esc cancel the question too, rather than quitting or going back.
		drive(tea.KeyMsg{Type: tea.KeyEnter})
		drive(tea.KeyMsg{Type: tea.KeyEsc})
		Expect(mainModel.currentPageID).To(Equal("summary"))
		Expect(summary().View()).ToNot(ContainSubstring("Type y to erase"))
	})

	It("starts the install page on enter then y", func() {
		drive(tea.KeyMsg{Type: tea.KeyEnter})
		drive(runes("y"))
		Expect(mainModel.currentPageID).To(Equal("install_process"))
	})
})

var _ = Describe("the extensions on the summary", func() {
	It("shows a catalog version, and live media for a file on the live media", func() {
		l := sdkLogger.NewBufferLogger(&bytes.Buffer{})
		mainModel = InitialModel(&l, "")
		mainModel.answers.Disk = "/dev/vda"
		mainModel.answers.Extensions = sdkExtensions.Extensions{{Name: "tailscale"}, {Name: "/run/initramfs/live/tools.sysext.raw"}}
		view := newSummaryPage().View()
		Expect(view).To(ContainSubstring("tailscale (latest)"))
		Expect(view).To(ContainSubstring("tools.sysext.raw (live media)"))
		Expect(view).ToNot(ContainSubstring("tools.sysext.raw (latest)"))
	})
})
