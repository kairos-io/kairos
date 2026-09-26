package splash

import (
	"errors"
	"testing"
)

// fakeConsole records the transitions and can be made to fail either one.
type fakeConsole struct {
	enters, leaves int
	enterErr       error
	leaveErr       error
}

func (f *fakeConsole) EnterLogs() error { f.enters++; return f.enterErr }
func (f *fakeConsole) LeaveLogs() error { f.leaves++; return f.leaveErr }

func TestEscapeTogglesBothWays(t *testing.T) {
	c := &fakeConsole{}
	m := newMachine(c)
	if m.Mode() != ModeSplash {
		t.Fatalf("starts in %v, want splash", m.Mode())
	}
	if !m.Feed([]byte{esc}) || m.Mode() != ModeLogs {
		t.Fatalf("after one Escape: mode %v", m.Mode())
	}
	if !m.Feed([]byte{esc}) || m.Mode() != ModeSplash {
		t.Fatalf("after two Escapes: mode %v", m.Mode())
	}
	if c.enters != 1 || c.leaves != 1 {
		t.Errorf("enters=%d leaves=%d, want 1 and 1", c.enters, c.leaves)
	}
}

func TestOrdinaryBytesDoNotToggle(t *testing.T) {
	c := &fakeConsole{}
	m := newMachine(c)
	if m.Feed([]byte("hello world\r\n\t ")) {
		t.Error("Feed reported a change")
	}
	if m.Mode() != ModeSplash || c.enters != 0 {
		t.Errorf("mode %v, enters %d", m.Mode(), c.enters)
	}
}

// An arrow key arrives as Escape, '[', 'A' in one read. Counting its Escape
// as a press would flip to the log view on any cursor key.
func TestArrowKeyIsNotAToggle(t *testing.T) {
	for _, seq := range []string{"\x1b[A", "\x1b[B", "\x1bOP", "\x1b[1;5D", "\x1b[200~"} {
		c := &fakeConsole{}
		m := newMachine(c)
		if m.Feed([]byte(seq)) {
			t.Errorf("%q reported a mode change", seq)
		}
		if m.Mode() != ModeSplash || c.enters != 0 {
			t.Errorf("%q toggled: mode %v enters %d", seq, m.Mode(), c.enters)
		}
	}
}

func TestEscapeAfterAControlSequenceStillToggles(t *testing.T) {
	c := &fakeConsole{}
	m := newMachine(c)
	if !m.Feed([]byte("\x1b[A\x1b")) || m.Mode() != ModeLogs {
		t.Fatalf("mode %v enters %d", m.Mode(), c.enters)
	}
	if c.enters != 1 {
		t.Errorf("enters = %d, want 1", c.enters)
	}
}

// A lone trailing Escape has no following byte to disambiguate it. On a boot
// console a press is far likelier than a split control sequence.
func TestTrailingEscapeIsAPress(t *testing.T) {
	c := &fakeConsole{}
	m := newMachine(c)
	if !m.Feed([]byte("x\x1b")) || m.Mode() != ModeLogs {
		t.Fatalf("mode %v", m.Mode())
	}
}

// Two presses inside one read must cancel out, not be collapsed into one.
func TestTwoEscapesInOneReadAreANoOp(t *testing.T) {
	c := &fakeConsole{}
	m := newMachine(c)
	changed := m.Feed([]byte{esc, esc})
	if !changed {
		t.Error("Feed should report that transitions ran")
	}
	if m.Mode() != ModeSplash {
		t.Errorf("mode %v, want splash", m.Mode())
	}
	if c.enters != 1 || c.leaves != 1 {
		t.Errorf("enters=%d leaves=%d, want 1 and 1", c.enters, c.leaves)
	}
}

func TestThreeEscapesLandOnLogs(t *testing.T) {
	c := &fakeConsole{}
	m := newMachine(c)
	m.Feed([]byte{esc, esc, esc})
	if m.Mode() != ModeLogs {
		t.Errorf("mode %v, want logs", m.Mode())
	}
}

// Nothing was shown, so there is nothing to undo: the animation stays up and
// the failure is recorded rather than leaving a blank screen.
func TestEnterLogsFailureStaysOnSplash(t *testing.T) {
	boom := errors.New("no /dev/kmsg")
	c := &fakeConsole{enterErr: boom}
	m := newMachine(c)
	if m.Feed([]byte{esc}) {
		t.Error("Feed reported a change that did not happen")
	}
	if m.Mode() != ModeSplash {
		t.Errorf("mode %v, want splash", m.Mode())
	}
	if errs := m.Errs(); len(errs) != 1 || !errors.Is(errs[0], boom) {
		t.Errorf("Errs() = %v", errs)
	}
	// And the next press must try again rather than latch off.
	c.enterErr = nil
	if !m.Feed([]byte{esc}) || m.Mode() != ModeLogs {
		t.Errorf("second press did not enter logs: mode %v", m.Mode())
	}
}

// Failing to leave costs a noisy kernel printing over the animation. Refusing
// to leave costs a console stuck on a log view with no way back, which is
// strictly worse, so the mode changes either way.
func TestLeaveLogsFailureStillReturnsToSplash(t *testing.T) {
	c := &fakeConsole{leaveErr: errors.New("close failed")}
	m := newMachine(c)
	m.Feed([]byte{esc})
	if m.Mode() != ModeLogs {
		t.Fatalf("setup: mode %v", m.Mode())
	}
	if !m.Feed([]byte{esc}) {
		t.Error("Feed did not report the change")
	}
	if m.Mode() != ModeSplash {
		t.Errorf("mode %v, want splash", m.Mode())
	}
	if len(m.Errs()) != 1 {
		t.Errorf("Errs() = %v, want the leave failure recorded", m.Errs())
	}
}

func TestModeString(t *testing.T) {
	if ModeSplash.String() != "splash" || ModeLogs.String() != "logs" {
		t.Errorf("%q %q", ModeSplash, ModeLogs)
	}
}

func TestEndOfSequenceStopsAtTheFinalByte(t *testing.T) {
	for _, tc := range []struct {
		in   string
		from int
		want int
	}{
		{"\x1b[A", 2, 3},
		{"\x1b[1;5DX", 2, 6},
		{"\x1b[12", 2, 4}, // unterminated: consume the rest
	} {
		if got := endOfSequence([]byte(tc.in), tc.from); got != tc.want {
			t.Errorf("endOfSequence(%q, %d) = %d, want %d", tc.in, tc.from, got, tc.want)
		}
	}
}
