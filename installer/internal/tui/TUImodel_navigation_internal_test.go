package tui

import (
	"testing"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// newTestModel gives each test its own model, since mainModel is package level.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	logger := sdkLogger.NewKairosLogger("installer-test", "info", true)
	mainModel = InitialModel(&logger, "")
	return &mainModel
}

// TestIssue3822_EscFromSummaryReturnsToInstallOptions covers the reported
// complaint: reaching the summary through "Customize Further" stacked up every
// configuration screen, so getting back to the branch point took one Esc per
// screen. See kairos-io/kairos#3822.
func TestIssue3822_EscFromSummaryReturnsToInstallOptions(t *testing.T) {
	m := newTestModel(t)
	m.navigationStack = []string{
		"prerequisites",
		"disk_selection",
		installOptionsPageID,
		"customization",
		"user_password",
		"ssh_keys",
	}
	m.currentPageID = summaryPageID

	m.goBack()

	if m.currentPageID != installOptionsPageID {
		t.Fatalf("one Esc from summary should land on %q, got %q", installOptionsPageID, m.currentPageID)
	}
	want := []string{"prerequisites", "disk_selection"}
	if len(m.navigationStack) != len(want) {
		t.Fatalf("stack should unwind to %v, got %v", want, m.navigationStack)
	}
	for i := range want {
		if m.navigationStack[i] != want[i] {
			t.Fatalf("stack should unwind to %v, got %v", want, m.navigationStack)
		}
	}
}

// TestIssue3822_EscFromSummaryViaStartInstall is the other path in the report:
// "Start Install" jumps straight to the summary, so the options page is already
// directly behind it. That case worked before and must keep working.
func TestIssue3822_EscFromSummaryViaStartInstall(t *testing.T) {
	m := newTestModel(t)
	m.navigationStack = []string{"prerequisites", "disk_selection", installOptionsPageID}
	m.currentPageID = summaryPageID

	m.goBack()

	if m.currentPageID != installOptionsPageID {
		t.Fatalf("expected %q, got %q", installOptionsPageID, m.currentPageID)
	}
	if len(m.navigationStack) != 2 {
		t.Fatalf("expected 2 entries left, got %v", m.navigationStack)
	}
}

// TestIssue3822_EscElsewhereStillGoesBackOnePage guards the behaviour that was
// deliberately kept: inside the customization flow, Esc is still one page at a
// time, so fixing a single answer costs one press.
func TestIssue3822_EscElsewhereStillGoesBackOnePage(t *testing.T) {
	m := newTestModel(t)
	m.navigationStack = []string{installOptionsPageID, "customization", "user_password"}
	m.currentPageID = "ssh_keys"

	m.goBack()

	if m.currentPageID != "user_password" {
		t.Fatalf("expected one page back to %q, got %q", "user_password", m.currentPageID)
	}
	if len(m.navigationStack) != 2 {
		t.Fatalf("expected 2 entries left, got %v", m.navigationStack)
	}
}

// TestIssue3822_EscOnEmptyStackIsANoop keeps the first page from popping off
// into an invalid state.
func TestIssue3822_EscOnEmptyStackIsANoop(t *testing.T) {
	m := newTestModel(t)
	m.navigationStack = nil
	m.currentPageID = summaryPageID

	m.goBack()

	if m.currentPageID != summaryPageID {
		t.Fatalf("expected the page to stay %q, got %q", summaryPageID, m.currentPageID)
	}
}

// TestIssue3822_SummaryUnwindStopsAtTheBottom covers a summary reached without
// ever passing the options page. The loop must stop rather than empty the stack.
func TestIssue3822_SummaryUnwindStopsAtTheBottom(t *testing.T) {
	m := newTestModel(t)
	m.navigationStack = []string{"prerequisites", "customization", "user_password"}
	m.currentPageID = summaryPageID

	m.goBack()

	if m.currentPageID != "prerequisites" {
		t.Fatalf("expected to land on %q, got %q", "prerequisites", m.currentPageID)
	}
	if len(m.navigationStack) != 0 {
		t.Fatalf("expected an empty stack, got %v", m.navigationStack)
	}
}

// TestInitCurrentPageFollowsCurrentPageID covers the defect the Esc handler
// carried: it called Init() on the page the user was leaving, because it
// reused the page index computed before the page changed.
func TestInitCurrentPageFollowsCurrentPageID(t *testing.T) {
	m := newTestModel(t)
	m.currentPageID = "nonexistent_page"
	if cmd := m.initCurrentPage(); cmd != nil {
		t.Fatalf("an unknown page id should produce no command, got one")
	}

	for _, p := range m.pages {
		m.currentPageID = p.ID()
		// Must not panic, and must resolve against currentPageID rather than
		// any stale index.
		_ = m.initCurrentPage()
	}
}
