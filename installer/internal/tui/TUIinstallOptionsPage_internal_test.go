package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// TestIssue4412_CustomizeFurtherKeepsDefaultFinishAction reproduces the
// reported bug: choosing "Customize Further" with the default after-install
// action selected used to leave mainModel.finishAction empty because only
// the "Start Install" branch set it. See kairos-io/kairos#4412.
func TestIssue4412_CustomizeFurtherKeepsDefaultFinishAction(t *testing.T) {
	logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
	mainModel = InitialModel(&logger, "")

	p := newInstallOptionsPage()
	p.cursor = 1 // "Customize Further"
	if _, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		cmd()
	}
	if mainModel.finishAction != "nothing" {
		t.Fatalf("expected finishAction to default to %q, got %q", "nothing", mainModel.finishAction)
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
	if mainModel.finishAction != "reboot" {
		t.Fatalf("expected finishAction to remain %q, got %q", "reboot", mainModel.finishAction)
	}
}

// TestIssue4412_CompletedInstallHelpNeverBlank guarantees the completed
// install page never renders the blank "System will  shortly" message that
// resulted from an empty finishAction reaching the page, and that a key
// press still exits in that case.
func TestIssue4412_CompletedInstallHelpNeverBlank(t *testing.T) {
	for _, finishAction := range []string{"nothing", "", "bogus"} {
		t.Run(finishAction, func(t *testing.T) {
			logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
			mainModel = InitialModel(&logger, "")
			mainModel.finishAction = finishAction
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
	mainModel.finishAction = "reboot"
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
