package splash

// Mode is what the console is currently showing.
type Mode int

const (
	// ModeSplash is the animation, with the kernel quiet.
	ModeSplash Mode = iota
	// ModeLogs is the kernel ring buffer, streaming.
	ModeLogs
)

func (m Mode) String() string {
	if m == ModeLogs {
		return "logs"
	}
	return "splash"
}

// esc is the byte a press of the Escape key puts on the console.
const esc = 0x1b

// Console is the side of the ESC toggle that touches the machine: quieting and
// unquieting the kernel, and streaming or stopping the kernel log. It is an
// interface so the state machine is testable without a tty, a kernel or PID 1.
type Console interface {
	// EnterLogs restores kernel printing and starts streaming the ring
	// buffer to the screen. It must replay the records already in the
	// buffer, not only the ones that arrive from now on, or pressing ESC
	// early in boot shows a blank screen.
	EnterLogs() error
	// LeaveLogs stops the streaming and quiets the kernel again.
	LeaveLogs() error
}

// machine is the ESC state machine. It starts in ModeSplash and toggles on
// every Escape key press.
//
// The invariant that matters: a failure never leaves the machine in a state
// the user cannot get out of. Failing to enter the log view keeps the
// animation up, and failing to leave it still returns to the animation, so
// ESC always does something.
type machine struct {
	mode Mode
	con  Console
	errs []error
}

func newMachine(con Console) *machine { return &machine{mode: ModeSplash, con: con} }

// Mode reports what is on screen.
func (m *machine) Mode() Mode { return m.mode }

// Errs returns the transition failures seen so far, oldest first.
func (m *machine) Errs() []error { return m.errs }

// toggle runs one transition and reports whether the mode changed.
func (m *machine) toggle() bool {
	if m.mode == ModeSplash {
		if err := m.con.EnterLogs(); err != nil {
			// Nothing was shown, so nothing has to be undone: stay put.
			m.errs = append(m.errs, err)
			return false
		}
		m.mode = ModeLogs
		return true
	}
	if err := m.con.LeaveLogs(); err != nil {
		// Go back to the animation anyway. The cost of a failure here is a
		// noisy kernel printing over the splash; the cost of refusing to
		// move is a console stuck on a log view with no way back.
		m.errs = append(m.errs, err)
	}
	m.mode = ModeSplash
	return true
}

// Feed processes one read from the console input and reports whether the mode
// changed. Multiple presses in a single read are applied in order, so an
// even number of them is correctly a no-op.
//
// A bare Escape is the toggle. Escape immediately followed by '[' or 'O' is
// the start of a control sequence (an arrow or function key, which the console
// sends as one burst) and is discarded up to and including its final byte, so
// an arrow key does not read as two presses. A trailing Escape at the end of
// the buffer is treated as a press: there is no further read coming to
// disambiguate it, and a user on a boot console pressing Escape is far more
// likely than one sending a split control sequence.
func (m *machine) Feed(p []byte) bool {
	changed := false
	for i := 0; i < len(p); i++ {
		if p[i] != esc {
			continue
		}
		if i+1 < len(p) && (p[i+1] == '[' || p[i+1] == 'O') {
			i = endOfSequence(p, i+2) - 1
			continue
		}
		if m.toggle() {
			changed = true
		}
	}
	return changed
}

// endOfSequence returns the index just past a CSI or SS3 sequence whose
// parameter bytes start at i. The final byte of such a sequence is in the
// range 0x40-0x7e; an unterminated sequence consumes the rest of the buffer.
func endOfSequence(p []byte, i int) int {
	for ; i < len(p); i++ {
		if p[i] >= 0x40 && p[i] <= 0x7e {
			return i + 1
		}
	}
	return len(p)
}
