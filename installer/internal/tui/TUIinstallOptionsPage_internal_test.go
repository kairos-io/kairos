package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/installer/internal/wizard"
)

// TestIssue4412_CustomizeFurtherKeepsDefaultFinishAction reproduces the
// reported bug: choosing "Customize Further" with the default after-install
// action selected used to leave the finish action unset because only the
// "Start Install" branch set it. See kairos-io/kairos#4412. The wizard spells
// "do nothing" as the empty string, so the test checks that the default
// choice went through Apply rather than being skipped.
func TestIssue4412_CustomizeFurtherKeepsDefaultFinishAction(t *testing.T) {
	logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
	mainModel = InitialModel(&logger, "")

	mainModel.answers.FinishAction = "stale"
	p := newInstallOptionsPage()
	p.cursor = 1 // "Customize Further"
	if _, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if mainModel.answers.FinishAction != "" {
		t.Fatalf("expected the finish action to default to nothing, got %q", mainModel.answers.FinishAction)
	}
	if normalizedFinishAction() != "nothing" {
		t.Fatalf("expected normalizedFinishAction to say nothing, got %q", normalizedFinishAction())
	}
}

// TestIssue4412_CustomizeFurtherPreservesSelectedFinishAction ensures a
// reboot/poweroff choice made before "Customize Further" survives the detour
// through the customization pages.
func TestIssue4412_CustomizeFurtherPreservesSelectedFinishAction(t *testing.T) {
	logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
	mainModel = InitialModel(&logger, "")

	p := newInstallOptionsPage()
	p.afterInstallIdx = 1 // "reboot"
	p.cursor = 1          // "Customize Further"
	if _, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if mainModel.answers.FinishAction != "reboot" {
		t.Fatalf("expected the finish action to remain %q, got %q", "reboot", mainModel.answers.FinishAction)
	}
}

// TestIssue4412_CompletedInstallHelpNeverBlank guarantees the completed
// install page never renders the blank "System will  shortly" message that
// resulted from an empty finish action reaching the page, and that a key
// press still exits in that case.
func TestIssue4412_CompletedInstallHelpNeverBlank(t *testing.T) {
	for _, finishAction := range []string{"nothing", "", "bogus"} {
		t.Run(finishAction, func(t *testing.T) {
			logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
			mainModel = InitialModel(&logger, "")
			mainModel.answers.FinishAction = finishAction
			mainModel.currentPageID = "install_process"

			ip := findInstallProcessPage(t)
			ip.progress = len(ip.steps) - 1

			help := ip.Help()
			if strings.Contains(help, "System will  ") || strings.TrimSpace(help) == "System will" {
				t.Fatalf("Help() rendered a blank action for finishAction=%q: %q", finishAction, help)
			}
			if help != "Press any key to exit" {
				t.Fatalf("expected the safe-fallback exit prompt for finishAction=%q, got %q", finishAction, help)
			}

			_, cmd := mainModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			if cmd == nil {
				t.Fatalf("expected a key press to quit for finishAction=%q", finishAction)
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("expected tea.QuitMsg for finishAction=%q", finishAction)
			}
		})
	}
}

// TestIssue4412_CompletedInstallBlocksKeysForRebootPoweroff keeps the
// existing behavior intact: once the install completes with a real
// reboot/poweroff action selected, key presses must not exit the TUI early
// (the agent process handles the reboot/poweroff itself).
func TestIssue4412_CompletedInstallBlocksKeysForRebootPoweroff(t *testing.T) {
	logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
	mainModel = InitialModel(&logger, "")
	mainModel.answers.FinishAction = "reboot"
	mainModel.currentPageID = "install_process"

	ip := findInstallProcessPage(t)
	ip.progress = len(ip.steps) - 1

	if help := ip.Help(); help != "System will reboot shortly" {
		t.Fatalf("expected reboot help text, got %q", help)
	}

	_, cmd := mainModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Fatalf("expected no cmd (keys blocked) while reboot is pending, got one")
	}
}

func findInstallProcessPage(t *testing.T) *installProcessPage {
	t.Helper()
	for _, p := range mainModel.pages {
		if ip, ok := p.(*installProcessPage); ok {
			return ip
		}
	}
	t.Fatal("install_process page not found in mainModel.pages")
	return nil
}

var _ = Describe("the install options page", func() {
	BeforeEach(func() {
		l := sdkLogger.NewKairosLogger("test", "error", false)
		mainModel = InitialModel(&l, "")
	})

	It("offers the finish step's choices by label and applies their value", func() {
		p := newInstallOptionsPage()
		view := p.View()
		Expect(view).To(ContainSubstring("Nothing"))
		Expect(view).To(ContainSubstring("Reboot"))
		Expect(view).To(ContainSubstring("Power off"))
		p.Update(tea.KeyMsg{Type: tea.KeyRight})
		p.Update(tea.KeyMsg{Type: tea.KeyRight})
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "summary"}))
		Expect(mainModel.answers.FinishAction).To(Equal(wizard.FinishPoweroff))
	})

	It("shows the current finish action when the page is opened again", func() {
		mainModel.answers.FinishAction = wizard.FinishReboot
		p := newInstallOptionsPage()
		p.Init()
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
		Expect(cmd).ToNot(BeNil())
		Expect(mainModel.answers.FinishAction).To(Equal(wizard.FinishReboot))
	})

	It("hides Customize Further and the optional steps behind the branding switch", func() {
		env := newFakeWizardEnv()
		env.noAdvanced = true
		useFakeWizardEnv(env)
		l := sdkLogger.NewKairosLogger("test", "error", false)
		mainModel = InitialModel(&l, "")
		p := newInstallOptionsPage()
		Expect(p.View()).ToNot(ContainSubstring("Customize Further"))
		for _, pg := range mainModel.pages {
			Expect(pg.ID()).ToNot(Equal(wizard.StepUser))
		}
		Expect(newSummaryPage().View()).ToNot(ContainSubstring("Username"))
	})

	It("is where the disk step leads, after the prerequisites and the install mode page", func() {
		ids := []string{}
		for _, pg := range mainModel.pages {
			ids = append(ids, pg.ID())
		}
		Expect(ids).To(Equal([]string{welcomePageID, "prerequisites", installModePageID, wizard.StepDisk, "install_options", "customization",
			wizard.StepUser, wizard.StepSSHKeys, wizard.StepHostname, wizard.StepLocale, wizard.StepExtensions,
			"summary", editPageID, "install_process", DebugBundlePageID}))
	})
})
