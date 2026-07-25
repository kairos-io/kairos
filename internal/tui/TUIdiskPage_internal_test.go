package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("diskSelectionPage Help", func() {
	It("advertises the debug-log hotkey", func() {
		p := newDiskSelectionPage()
		Expect(p.Help()).To(ContainSubstring("ctrl+d"))
	})
})

func makeDiskPage(n int) *diskSelectionPage {
	disks := make([]diskStruct, n)
	for i := 0; i < n; i++ {
		disks[i] = diskStruct{id: i, name: "/dev/sd" + string(rune('a'+i)), size: "10.00 GiB"}
	}
	return &diskSelectionPage{disks: disks}
}

func pressDown(p *diskSelectionPage) {
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
}

func pressUp(p *diskSelectionPage) {
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
}

// cursorVisible reports whether the cursor index falls inside the scroll window.
func cursorVisible(p *diskSelectionPage) bool {
	vc := p.visibleCount()
	return p.cursor >= p.offset && p.cursor < p.offset+vc
}

func TestDiskScrollCursorStaysVisible(t *testing.T) {
	// Small terminal so the disk list overflows: height 24 -> ~10 visible rows.
	mainModel = Model{width: 80, height: 24}
	p := makeDiskPage(30)

	if len(p.disks) <= p.visibleCount() {
		t.Fatalf("test precondition: list (%d) should overflow visible window (%d)", len(p.disks), p.visibleCount())
	}

	// Walk all the way down. Cursor must remain inside the window at every step.
	for i := 0; i < len(p.disks)-1; i++ {
		pressDown(p)
		if !cursorVisible(p) {
			t.Fatalf("cursor %d not visible after %d downs: offset=%d visible=%d", p.cursor, i+1, p.offset, p.visibleCount())
		}
	}
	if p.cursor != len(p.disks)-1 {
		t.Fatalf("expected cursor at last disk %d, got %d", len(p.disks)-1, p.cursor)
	}

	// Walk back up. Same invariant.
	for i := 0; i < len(p.disks)-1; i++ {
		pressUp(p)
		if !cursorVisible(p) {
			t.Fatalf("cursor %d not visible after going up: offset=%d visible=%d", p.cursor, p.offset, p.visibleCount())
		}
	}
	if p.cursor != 0 || p.offset != 0 {
		t.Fatalf("expected cursor/offset back at 0, got cursor=%d offset=%d", p.cursor, p.offset)
	}
}

func TestDiskScrollNoOverflowSmallList(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makeDiskPage(3)

	// No scroll indicators when everything fits.
	out := p.View()
	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Fatalf("did not expect scroll indicators for a short list:\n%s", out)
	}
	if p.offset != 0 {
		t.Fatalf("offset should stay 0 for short list, got %d", p.offset)
	}
}

// stubScanDisks swaps the package-level scanDisks for the given sequence of
// responses, one per call, and returns a cleanup that restores the original.
// It also asserts (via the returned counter) how many times it was invoked.
func stubScanDisks(t *testing.T, sequence [][]diskStruct) (calls *int, restore func()) {
	t.Helper()
	original := scanDisks
	var count int
	scanDisks = func() ([]diskStruct, error) {
		i := count
		count++
		if i >= len(sequence) {
			// Last stub is repeated for any extra calls.
			i = len(sequence) - 1
		}
		out := make([]diskStruct, len(sequence[i]))
		copy(out, sequence[i])
		return out, nil
	}
	return &count, func() { scanDisks = original }
}

// TestIssue4260_DiskListRefreshedOnPageInit demonstrates the reported bug and
// its fix: when a prerequisites plugin (e.g. wipefs) mutates disk state
// between installer startup and the moment the user reaches disk selection,
// the TUI must refresh its cached disk list so the user is not choosing from
// stale entries. See kairos-io/kairos#4260.
func TestIssue4260_DiskListRefreshedOnPageInit(t *testing.T) {
	mainModel = Model{width: 80, height: 24}

	initial := []diskStruct{
		{id: 0, name: "/dev/sda", size: "10.00 GiB"},
		{id: 1, name: "/dev/sdb", size: "20.00 GiB"},
		{id: 2, name: "/dev/sdc", size: "30.00 GiB"},
	}
	// After wipefs runs, /dev/sdb is gone.
	afterWipe := []diskStruct{
		{id: 0, name: "/dev/sda", size: "10.00 GiB"},
		{id: 1, name: "/dev/sdc", size: "30.00 GiB"},
	}

	calls, restore := stubScanDisks(t, [][]diskStruct{initial, afterWipe})
	defer restore()

	// Startup: TUI builds the disk page during InitialModel.
	p := newDiskSelectionPage()
	if p == nil {
		t.Fatal("newDiskSelectionPage returned nil")
	}
	if len(p.disks) != len(initial) {
		t.Fatalf("initial construction: want %d disks, got %d", len(initial), len(p.disks))
	}
	if *calls != 1 {
		t.Fatalf("expected 1 scan on construction, got %d", *calls)
	}

	// User navigates back to prerequisites and applies a wipefs plugin, then
	// returns to disk selection. Bubble Tea invokes Init on the destination
	// page as part of GoToPageMsg handling. The page must re-scan.
	p.cursor = 2 // was pointing at /dev/sdc (index 2 in the initial list)
	if cmd := p.Init(); cmd != nil {
		// The refresh must not schedule any follow-up tea.Cmd.
		t.Fatalf("Init must not return a tea.Cmd, got %T", cmd)
	}
	if len(p.disks) != len(afterWipe) {
		t.Fatalf("post-refresh: want %d disks, got %d (list not refreshed — bug #4260)", len(afterWipe), len(p.disks))
	}
	if p.disks[0].name != "/dev/sda" || p.disks[1].name != "/dev/sdc" {
		t.Fatalf("post-refresh: unexpected disk set %+v", p.disks)
	}
	if p.cursor < 0 || p.cursor >= len(p.disks) {
		t.Fatalf("post-refresh: cursor %d not clamped to new range (len=%d)", p.cursor, len(p.disks))
	}
	if p.offset != 0 {
		t.Fatalf("post-refresh: scroll offset not reset, got %d", p.offset)
	}
	if *calls != 2 {
		t.Fatalf("expected 2 scans total (construct + refresh), got %d", *calls)
	}
}

// TestIssue4260_DiskListRefreshHandlesGrowingSet covers the other side of the
// same coin: a prerequisites plugin can also *reveal* disks that were not
// visible before (e.g. by removing an encrypted overlay). The refresh must
// pick them up.
func TestIssue4260_DiskListRefreshHandlesGrowingSet(t *testing.T) {
	mainModel = Model{width: 80, height: 24}

	before := []diskStruct{{id: 0, name: "/dev/sda", size: "10.00 GiB"}}
	after := []diskStruct{
		{id: 0, name: "/dev/sda", size: "10.00 GiB"},
		{id: 1, name: "/dev/nvme0n1", size: "500.00 GiB"},
	}

	_, restore := stubScanDisks(t, [][]diskStruct{before, after})
	defer restore()

	p := newDiskSelectionPage()
	if len(p.disks) != 1 {
		t.Fatalf("initial: want 1 disk, got %d", len(p.disks))
	}
	p.Init()
	if len(p.disks) != 2 {
		t.Fatalf("post-refresh: want 2 disks after growing set, got %d", len(p.disks))
	}
	if p.disks[1].name != "/dev/nvme0n1" {
		t.Fatalf("new disk not picked up on refresh: %+v", p.disks)
	}
}

// TestIssue4260_InitScanErrorKeepsPreviousList guarantees that a transient
// scan failure on refresh does not blank the list the user was looking at.
// Better to show slightly stale data than nothing.
func TestIssue4260_InitScanErrorKeepsPreviousList(t *testing.T) {
	mainModel = Model{width: 80, height: 24}

	original := scanDisks
	defer func() { scanDisks = original }()

	initial := []diskStruct{
		{id: 0, name: "/dev/sda", size: "10.00 GiB"},
		{id: 1, name: "/dev/sdb", size: "20.00 GiB"},
	}
	firstCall := true
	scanDisks = func() ([]diskStruct, error) {
		if firstCall {
			firstCall = false
			out := make([]diskStruct, len(initial))
			copy(out, initial)
			return out, nil
		}
		return nil, fmt.Errorf("simulated ghw failure")
	}

	p := newDiskSelectionPage()
	if len(p.disks) != 2 {
		t.Fatalf("initial: want 2 disks, got %d", len(p.disks))
	}
	p.Init()
	if len(p.disks) != 2 {
		t.Fatalf("scan error must keep previous list, got %d disks", len(p.disks))
	}
}

func TestDiskScrollIndicatorsAndViewBounds(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makeDiskPage(30)

	// At the top: only the "below" indicator.
	top := p.View()
	if strings.Contains(top, "more above") {
		t.Fatalf("unexpected 'more above' at top of list:\n%s", top)
	}
	if !strings.Contains(top, "more below") {
		t.Fatalf("expected 'more below' at top of long list:\n%s", top)
	}

	// Move into the middle: both indicators present.
	for i := 0; i < 15; i++ {
		pressDown(p)
	}
	mid := p.View()
	if !strings.Contains(mid, "more above") || !strings.Contains(mid, "more below") {
		t.Fatalf("expected both indicators in the middle:\n%s", mid)
	}

	// The rendered content must never exceed the Model.View slice budget
	// (height-10). strings.Count counts trailing-newline-terminated rows.
	_, h := effectiveSize(mainModel.width, mainModel.height)
	budget := h - 10
	if got := strings.Count(mid, "\n"); got > budget {
		t.Fatalf("rendered %d content lines, exceeds budget %d", got, budget)
	}
}
