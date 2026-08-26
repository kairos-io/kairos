package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-installer/prereqs"
)

// makePrereqMultiPage builds a loaded prerequisites page holding a single
// multiselect check with n options — the shape a check plugin returns when it
// finds many candidates (e.g. the diskwipe plugin listing every disk that
// carries leftover Kairos partitions).
func makePrereqMultiPage(n int) *prerequisitesPage {
	opts := make([]prereqs.Option, n)
	for i := range opts {
		opts[i] = prereqs.Option{
			ID:    fmt.Sprintf("/dev/sd%d", i),
			Label: fmt.Sprintf("/dev/sd%d (COS_STATE, COS_OEM)", i),
		}
	}
	p := newPrerequisitesPage()
	p.checks = []prereqs.Check{{
		ID:         "diskwipe",
		Title:      "Leftover Kairos data detected",
		Message:    fmt.Sprintf("%d disk(s) hold partitions from a previous installation", n),
		Severity:   prereqs.SeverityWarning,
		Prompt:     prereqs.PromptMultiSelect,
		PromptText: "Select disks to wipe (all data on them will be destroyed):",
		Options:    opts,
	}}
	p.buildFields()
	p.loaded = true
	return p
}

// prereqFocusVisible reports whether the focused field's line falls inside the
// resolved scroll window.
func prereqFocusVisible(p *prerequisitesPage) bool {
	_, focus, offset, vc := p.window()
	return focus >= offset && focus < offset+vc
}

// TestPrereqScrollCursorStaysVisible walks the cursor across every option of a
// multiselect check that overflows the viewport and asserts the focused line
// never leaves the scroll window — the prerequisites analogue of the disk-list
// scroll fix (kairos-agent#1237).
func TestPrereqScrollCursorStaysVisible(t *testing.T) {
	// Small terminal so a long option list overflows the content area.
	mainModel = Model{width: 80, height: 24}
	p := makePrereqMultiPage(40)

	region, _, _, vc := p.window()
	if len(region) <= vc {
		t.Fatalf("test precondition: region (%d lines) should overflow visible window (%d)", len(region), vc)
	}
	if len(p.fields) != 40 {
		t.Fatalf("expected 40 option fields, got %d", len(p.fields))
	}

	// Walk all the way down. The focused line must remain visible at every step.
	for i := 0; i < len(p.fields)-1; i++ {
		p.moveCursor(1)
		if !prereqFocusVisible(p) {
			_, focus, offset, vc := p.window()
			t.Fatalf("focus line %d not visible after %d downs: offset=%d vc=%d", focus, i+1, offset, vc)
		}
	}
	if p.cursor != len(p.fields)-1 {
		t.Fatalf("expected cursor at last option %d, got %d", len(p.fields)-1, p.cursor)
	}

	// Walk back up. Same invariant.
	for i := 0; i < len(p.fields)-1; i++ {
		p.moveCursor(-1)
		if !prereqFocusVisible(p) {
			_, focus, offset, vc := p.window()
			t.Fatalf("focus line %d not visible going up: offset=%d vc=%d", focus, offset, vc)
		}
	}
	if p.cursor != 0 {
		t.Fatalf("expected cursor back at 0, got %d", p.cursor)
	}
}

// TestPrereqFirstEntryShowsTitle guards the common single-multiselect case (the
// diskwipe plugin): when the page is first shown, the scroll window starts at
// the top so the check title and prompt are visible, even when the option list
// overflows.
func TestPrereqFirstEntryShowsTitle(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makePrereqMultiPage(40)

	out := p.View()
	if p.offset != 0 {
		t.Fatalf("expected offset 0 on first entry, got %d", p.offset)
	}
	if !strings.Contains(out, "Leftover Kairos data detected") {
		t.Fatalf("check title not visible on first entry:\n%s", out)
	}
	if strings.Contains(out, "more above") {
		t.Fatalf("unexpected 'more above' on first entry:\n%s", out)
	}
}

// TestPrereqScrollNoOverflowSmallList checks a check that fits entirely shows no
// scroll indicators and never scrolls.
func TestPrereqScrollNoOverflowSmallList(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makePrereqMultiPage(2)

	out := p.View()
	if strings.Contains(out, "more above") || strings.Contains(out, "more below") {
		t.Fatalf("did not expect scroll indicators for a short list:\n%s", out)
	}
	if p.offset != 0 {
		t.Fatalf("offset should stay 0 for short list, got %d", p.offset)
	}
}

// TestPrereqScrollIndicatorsAndViewBounds asserts the scroll indicators appear
// at the right times and the rendered content never exceeds Model.View's slice
// budget (height-10), which is what caused the cursor to disappear before.
func TestPrereqScrollIndicatorsAndViewBounds(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makePrereqMultiPage(40)

	// At the top: only the "below" indicator.
	top := p.View()
	if strings.Contains(top, "more above") {
		t.Fatalf("unexpected 'more above' at top of list:\n%s", top)
	}
	if !strings.Contains(top, "more below") {
		t.Fatalf("expected 'more below' at top of long list:\n%s", top)
	}

	// Move into the middle: both indicators present.
	for i := 0; i < 20; i++ {
		p.moveCursor(1)
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
		t.Fatalf("rendered %d content lines, exceeds budget %d:\n%s", got, budget, mid)
	}
}

// TestPrereqFieldLineOffsetMapsToRenderedOption validates the line-offset math
// against the real box render: for every option, the computed line must be the
// rendered line actually holding that option's label.
func TestPrereqFieldLineOffsetMapsToRenderedOption(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makePrereqMultiPage(6)

	c := p.checks[0]
	contentW := p.boxWidth()
	lines := strings.Split(p.renderBox(c, p.checkInner(0, c), contentW), "\n")

	for oi := 0; oi < len(c.Options); oi++ {
		p.cursor = oi                             // fields map 1:1 to options for a single multiselect
		idx := 1 + p.fieldLineOffset(c, contentW) // +1 for the top border line
		if idx < 0 || idx >= len(lines) {
			t.Fatalf("option %d: computed line %d out of range [0,%d)", oi, idx, len(lines))
		}
		if !strings.Contains(lines[idx], c.Options[oi].Label) {
			t.Fatalf("option %d: computed line %d = %q does not hold label %q\nbox:\n%s",
				oi, idx, lines[idx], c.Options[oi].Label, strings.Join(lines, "\n"))
		}
	}
}

// TestPrereqScrollShowsApplyError makes sure an apply error is scrolled into
// view even when the option list above it overflows the viewport.
func TestPrereqScrollShowsApplyError(t *testing.T) {
	mainModel = Model{width: 80, height: 24}
	p := makePrereqMultiPage(40)
	p.failure = failRequired
	p.applyErr = "Required action(s) failed. Fix the issue and retry."

	out := p.View()
	if !strings.Contains(out, "Required action(s) failed") {
		t.Fatalf("apply error not scrolled into view:\n%s", out)
	}
}
