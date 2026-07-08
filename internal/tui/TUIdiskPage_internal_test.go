package tui

import (
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
