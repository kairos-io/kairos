package splash

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A serial console, a kernel with no CONFIG_VT, or a redirected stdout: the
// animation is impossible and the splash must still say it ran, on one line,
// with exit success.
func TestRunFallsBackWhenNotATTY(t *testing.T) {
	var out bytes.Buffer
	err := Run(Options{
		Out:       &out,
		Rows:      40,
		Cols:      120,
		IsTTY:     false,
		MaxFrames: 1,
		Branding:  DefaultBranding(),
		Version:   "version: v4.3.0",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := out.String(); got != "KAIROS  version: v4.3.0\n" {
		t.Errorf("output = %q", got)
	}
}

func TestRunFallbackOmitsTheVersionWhenUnknown(t *testing.T) {
	var out bytes.Buffer
	if err := Run(Options{Out: &out, IsTTY: false, Branding: DefaultBranding(), MaxFrames: 1}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "KAIROS\n" {
		t.Errorf("output = %q", got)
	}
}

func TestRunFallbackUsesTheBrandedName(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	b.Name = "HADRON"
	if err := Run(Options{Out: &out, IsTTY: false, Branding: b, MaxFrames: 1}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "HADRON\n" {
		t.Errorf("output = %q", got)
	}
}

// A console that is a tty but smaller than the wordmark gets the same one
// line, not a wordmark clipped down the middle.
func TestRunFallsBackWhenTheConsoleIsTooSmall(t *testing.T) {
	b := DefaultBranding()
	for _, tc := range []struct{ rows, cols int }{
		{b.Height() + 5, b.Width() + 2}, // one row short
		{b.Height() + 6, b.Width() + 1}, // one column short
		{24, 40},
		{2, 200},
	} {
		var out bytes.Buffer
		if err := Run(Options{Out: &out, Rows: tc.rows, Cols: tc.cols, IsTTY: true, Branding: b, MaxFrames: 1, Frame: time.Millisecond, Now: fixedClock(time.Millisecond)}); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); got != "KAIROS\n" {
			t.Errorf("%dx%d: output = %q, want the fallback line", tc.rows, tc.cols, got)
		}
	}
}

func TestFitsAcceptsTheSmallestUsableConsole(t *testing.T) {
	b := DefaultBranding()
	if !b.Fits(b.Height()+6, b.Width()+2) {
		t.Error("the smallest console the animation claims to support does not Fit")
	}
	// A standard 80x24 has to work; it is what a bare VGA console gives.
	if !b.Fits(24, 80) {
		t.Error("80x24 does not Fit, so the default branding would never animate")
	}
}

// fixedClock advances by exactly one frame per call, so the animation is
// deterministic and a test can assert on what a given frame contains.
func fixedClock(step time.Duration) func() time.Time {
	base := time.Unix(1700000000, 0)
	n := 0
	return func() time.Time {
		t := base.Add(time.Duration(n) * step)
		n++
		return t
	}
}

func TestRunPaintsTheWordmarkAndTagline(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	err := Run(Options{
		Out:       &out,
		Rows:      30,
		Cols:      100,
		IsTTY:     true,
		Branding:  b,
		Version:   "version: v4.3.0",
		Frame:     time.Millisecond,
		Now:       fixedClock(33 * time.Millisecond),
		MaxFrames: 1,
		Seed:      7,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, seqEnter) {
		t.Error("did not enter the alternate screen first")
	}
	if !strings.HasSuffix(got, seqLeave) {
		t.Error("did not restore the screen on the way out")
	}
	// Every glyph of every wordmark row has to reach the console. They are
	// written cell by cell with cursor moves in between, so assert on the
	// glyphs rather than on the rows as strings.
	for _, row := range b.Wordmark {
		for _, r := range row {
			if r == ' ' {
				continue
			}
			if !strings.ContainsRune(got, r) {
				t.Fatalf("glyph %q never written", r)
			}
		}
	}
	for _, word := range strings.Fields(b.Tagline) {
		for _, r := range word {
			if !strings.ContainsRune(got, r) {
				t.Fatalf("tagline rune %q never written", r)
			}
		}
	}
}

// The regression that matters on a real boot console: a 256-colour code looks
// right in a terminal emulator and paints the wrong hue on tty1.
func TestRunEmitsNo256ColorSequences(t *testing.T) {
	var out bytes.Buffer
	err := Run(Options{
		Out: &out, Rows: 30, Cols: 100, IsTTY: true,
		Branding: DefaultBranding(), Version: "version: v4.3.0",
		Frame: time.Millisecond, Now: fixedClock(33 * time.Millisecond),
		MaxFrames: 12, Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "38;5;") {
		t.Error("emitted a 256-colour SGR sequence")
	}
	if strings.Contains(out.String(), "38;2;") {
		t.Error("emitted a truecolour SGR sequence")
	}
}

// A console wide enough for the wordmark but not the tagline gets the
// wordmark and no tagline, rather than a tagline cut off mid-word.
func TestRunSkipsTheTaglineWhenItDoesNotFit(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	b.Tagline = strings.Repeat("x", 200)
	err := Run(Options{
		Out: &out, Rows: 30, Cols: 60, IsTTY: true, Branding: b,
		Frame: time.Millisecond, Now: fixedClock(33 * time.Millisecond),
		MaxFrames: 1, Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "xx") {
		t.Error("painted part of a tagline that does not fit")
	}
	if !strings.ContainsRune(out.String(), '█') {
		t.Error("did not paint the wordmark")
	}
}

func TestRunStopsWhenDoneFires(t *testing.T) {
	done := make(chan struct{})
	close(done)
	var out bytes.Buffer
	start := time.Now()
	err := Run(Options{
		Out: &out, Rows: 30, Cols: 100, IsTTY: true,
		Branding: DefaultBranding(),
		Frame:    time.Hour, // a tick would never arrive
		Done:     done,
		Seed:     7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v to honour Done", elapsed)
	}
	if !strings.HasSuffix(out.String(), seqLeave) {
		t.Error("did not restore the screen")
	}
}

func TestRunNeedsAnOutput(t *testing.T) {
	if err := Run(Options{}); err == nil {
		t.Error("Run with no output succeeded")
	}
}

func TestRunFallsBackToDefaultBrandingWhenGivenNone(t *testing.T) {
	var out bytes.Buffer
	if err := Run(Options{Out: &out, IsTTY: false, MaxFrames: 1}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "KAIROS\n" {
		t.Errorf("output = %q", got)
	}
}

func TestReadOSVersion(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, body, want string
	}{
		{"quoted", "NAME=Kairos\nVERSION_ID=\"v4.3.0\"\n", "version: v4.3.0"},
		{"bare", "VERSION_ID=v4.3.0\n", "version: v4.3.0"},
		{"single-quoted", "VERSION_ID='v4.3.0'\n", "version: v4.3.0"},
		{"absent", "NAME=Kairos\n", ""},
		{"empty", "VERSION_ID=\"\"\n", ""},
		{"long", "VERSION_ID=" + strings.Repeat("9", 60) + "\n", "version: " + strings.Repeat("9", 40)},
		// A key that merely ends in VERSION_ID must not match.
		{"suffix-only", "IMAGE_VERSION_ID=v9\n", ""},
	} {
		p := filepath.Join(dir, tc.name)
		if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ReadOSVersion(p); got != tc.want {
			t.Errorf("%s: ReadOSVersion = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := ReadOSVersion(filepath.Join(dir, "absent-file")); got != "" {
		t.Errorf("missing file returned %q", got)
	}
}

// The diff is what keeps the animation from tearing on a slow virtual
// console: a second Flush with nothing repainted must write nothing at all.
func TestGridFlushWritesOnlyChanges(t *testing.T) {
	var out bytes.Buffer
	g := newGrid(&out, 4, 10)
	g.paintRunes(1, 2, "hi", 94)
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}
	first := out.Len()
	if first == 0 {
		t.Fatal("first Flush wrote nothing")
	}
	out.Reset()
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("second Flush wrote %d bytes: %q", out.Len(), out.String())
	}
	// invalidate is what the ESC round trip relies on, because the log view
	// scrolled the screen and the buffer no longer describes it.
	g.invalidate()
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Error("Flush after invalidate wrote nothing, so the splash would not come back")
	}
}

func TestGridIgnoresOutOfRangeWrites(t *testing.T) {
	var out bytes.Buffer
	g := newGrid(&out, 3, 3)
	g.set(-1, 0, "x", 31, 0)
	g.set(0, -1, "x", 31, 0)
	g.set(3, 0, "x", 31, 0)
	g.set(0, 3, "x", 31, 0)
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "x") {
		t.Error("an out of range write landed in the buffer")
	}
}

func TestPaintCenteredReportsWhetherItFit(t *testing.T) {
	var out bytes.Buffer
	g := newGrid(&out, 3, 10)
	if !g.paintCentered(0, "abc", 31) {
		t.Error("a string that fits reported that it did not")
	}
	if g.paintCentered(1, strings.Repeat("y", 11), 31) {
		t.Error("a string that does not fit reported that it did")
	}
	if !g.paintCentered(2, "", 31) {
		t.Error("an empty string reported that it did not fit")
	}
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "y") {
		t.Error("painted an over-wide string anyway")
	}
}

// Trails have to age out, or the orbiting dots leave permanent smears over
// the wordmark.
func TestTrailsDecayToNothing(t *testing.T) {
	var out bytes.Buffer
	g := newGrid(&out, 3, 3)
	g.set(1, 1, "o", 94, trailLife)
	for i := 0; i < trailLife+1; i++ {
		g.decay()
		g.paintTrails()
	}
	c := g.cur[1*3+1]
	if c.glyph != " " || c.color != 0 || c.intensity != 0 {
		t.Errorf("cell still %+v after %d frames", c, trailLife+1)
	}
}

func TestParticlesOrbitOutsideTheWordmark(t *testing.T) {
	b := DefaultBranding()
	parts := newParticles(30, 100, b, 12345)
	if len(parts) != numParticles {
		t.Fatalf("got %d particles, want %d", len(parts), numParticles)
	}
	minRX := float64(b.Width())/2 + 4
	minRY := float64(b.Height())/2 + 3
	for i, p := range parts {
		if p.rx < minRX || p.ry < minRY {
			t.Errorf("particle %d orbits inside the wordmark: rx=%v ry=%v", i, p.rx, p.ry)
		}
		if p.omega <= 0 {
			t.Errorf("particle %d does not move: omega=%v", i, p.omega)
		}
	}
}

// Same seed, same orbits: the seed is the only reason a test of the painted
// frame is stable at all.
func TestParticlesAreReproducibleFromTheSeed(t *testing.T) {
	a := newParticles(30, 100, DefaultBranding(), 99)
	b := newParticles(30, 100, DefaultBranding(), 99)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("particle %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
	c := newParticles(30, 100, DefaultBranding(), 100)
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
		}
	}
	if same {
		t.Error("a different seed produced identical orbits")
	}
}

// Runs of changed cells must not cross a row boundary. A terminal with
// autowrap on would put the next character in the right place anyway, right up
// until the bottom-right cell, where writing scrolls the screen and drags the
// whole animation up by a line on the very first frame.
func TestGridNeverReliesOnAutowrap(t *testing.T) {
	var out bytes.Buffer
	g := newGrid(&out, 3, 4)
	// The first Flush is the worst case: every cell differs from the empty
	// prev buffer, so it is one unbroken run of 12 cells.
	if err := g.Flush(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for row := 1; row <= 3; row++ {
		want := "\x1b[" + itoa(row) + ";1H"
		if !strings.Contains(got, want) {
			t.Errorf("no cursor move to row %d (%q) in %q", row, want, got)
		}
	}
	// And nothing may be written past the last column of a row without a
	// move first: count the characters emitted between cursor moves.
	for _, chunk := range strings.Split(got, "\x1b[")[1:] {
		_, body, ok := strings.Cut(chunk, "H")
		if !ok {
			continue
		}
		if n := len(strings.Split(body, "\x1b")[0]); n > g.cols {
			t.Errorf("wrote %d cells in one run on a %d column grid", n, g.cols)
		}
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// countingClock is fixedClock with the call count exposed. Run calls Now once
// for the start instant and once per frame, so the count is the only
// deterministic frame counter available: grid.Flush buffers, so the number of
// writes on Out says nothing about how many frames were painted.
func countingClock(step time.Duration, calls *int) func() time.Time {
	base := time.Unix(1700000000, 0)
	return func() time.Time {
		t := base.Add(time.Duration(*calls) * step)
		*calls++
		return t
	}
}

// The booted-system unit is a oneshot ordered before getty.target, so getty
// waits for it: the animation has to end on its own there or the login prompt
// never arrives. Done is nil and MaxFrames is unset here, which is exactly how
// that unit runs, so a Duration that does not stop the loop hangs this test
// rather than failing it.
func TestRunStopsAfterTheDuration(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	calls := 0
	err := Run(Options{
		Out:      &out,
		Rows:     b.Height() + 8,
		Cols:     b.Width() + 10,
		IsTTY:    true,
		Branding: b,
		Frame:    time.Millisecond,
		Duration: 5 * time.Millisecond,
		Now:      countingClock(time.Millisecond, &calls),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// One call for the start instant, then one per frame. The fifth frame
	// reads 5ms of elapsed time, which is the whole budget, and returns
	// before painting: the deadline is inclusive so a Duration shorter than
	// one frame cannot still paint a frame.
	if calls != 6 {
		t.Errorf("Now called %d times, want 6 (start + 5 frames)", calls)
	}
	if !strings.Contains(out.String(), seqLeave) {
		t.Error("the console was not restored on the way out")
	}
}

// Zero is the initramfs unit: it animates until switch-root sends SIGTERM, so
// a zero Duration must not be read as "stop immediately".
func TestRunTreatsAZeroDurationAsUnlimited(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	calls := 0
	err := Run(Options{
		Out:       &out,
		Rows:      b.Height() + 8,
		Cols:      b.Width() + 10,
		IsTTY:     true,
		Branding:  b,
		Frame:     time.Millisecond,
		MaxFrames: 3,
		Now:       countingClock(time.Hour, &calls),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 4 {
		t.Errorf("Now called %d times, want 4 (start + 3 frames)", calls)
	}
}

// maxRowAddressed returns the highest row a cursor-position sequence in s
// names, which is the only evidence on the wire that the buffer was rebuilt
// for a taller console: the grid never addresses a row it does not have.
func maxRowAddressed(s string) int {
	max := 0
	for _, chunk := range strings.Split(s, "\x1b[") {
		semi := strings.IndexByte(chunk, ';')
		if semi <= 0 || !strings.Contains(chunk[semi:], "H") {
			continue
		}
		row := 0
		for _, r := range chunk[:semi] {
			if r < '0' || r > '9' {
				row = 0
				break
			}
			row = row*10 + int(r-'0')
		}
		if row > max {
			max = row
		}
	}
	return max
}

// The boot this comes from: the initramfs has no DRM driver, so the first
// frames draw on the BIOS VGA text console at 80x25. Shortly after the booted
// system starts, bochs-drm binds and fbcon switches the same tty to 160x50.
// The animation read the size once at startup, so it kept painting an 80x25
// layout in the top-left corner of a console four times that size.
func TestRunFollowsAConsoleThatGainsAFramebuffer(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	const smallRows, smallCols = 25, 80
	const bigRows, bigCols = 50, 160
	polls := 0
	err := Run(Options{
		Out: &out, Rows: smallRows, Cols: smallCols, IsTTY: true, Branding: b,
		Frame: time.Millisecond, Now: fixedClock(33 * time.Millisecond),
		MaxFrames: 4, Seed: 7,
		Size: func() (int, int) {
			polls++
			if polls <= 2 {
				return smallRows, smallCols
			}
			return bigRows, bigCols
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	if n := strings.Count(got, seqEnter); n != 2 {
		t.Errorf("entered the alternate screen %d times, want 2 (once at start, once to clear the old layout)", n)
	}
	if row := maxRowAddressed(got); row <= smallRows {
		t.Errorf("highest row addressed is %d, so the buffer was never rebuilt for the %dx%d console", row, bigRows, bigCols)
	}
	_, after, found := strings.Cut(got[strings.Index(got, seqEnter)+len(seqEnter):], seqEnter)
	if !found || !strings.ContainsRune(after, '█') {
		t.Error("nothing was repainted after the console grew")
	}
}

// A console that shrinks below the wordmark gets the same treatment as one
// that was too small to begin with: stop, rather than paint a wordmark cut
// down the middle. The deferred leave puts the console back.
func TestRunStopsWhenTheConsoleShrinksBelowTheWordmark(t *testing.T) {
	var out bytes.Buffer
	b := DefaultBranding()
	calls := 0
	err := Run(Options{
		Out: &out, Rows: 30, Cols: 100, IsTTY: true, Branding: b,
		Frame: time.Millisecond, Now: countingClock(time.Millisecond, &calls),
		MaxFrames: 50, Seed: 7,
		Size: func() (int, int) {
			if calls <= 2 {
				return 30, 100
			}
			return 10, 30
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// One call for the start instant, one for the frame that still fits,
	// and one for the frame that reads the smaller console and returns.
	if calls != 3 {
		t.Errorf("Now called %d times, want 3: the loop kept running on a console too small for the wordmark", calls)
	}
	if !strings.HasSuffix(out.String(), seqLeave) {
		t.Error("the console was not restored on the way out")
	}
}

// term.GetSize fails when the console goes away mid-boot. Rebuilding the
// buffer for a zero-sized grid would stop the animation on a console that is
// still perfectly drawable, so a failed query keeps the geometry in hand.
func TestRunKeepsTheGridWhenTheConsoleSizeIsUnreadable(t *testing.T) {
	for name, size := range map[string]func() (int, int){
		"unreadable": func() (int, int) { return 0, 0 },
		"negative":   func() (int, int) { return -1, -1 },
		"unchanged":  func() (int, int) { return 30, 100 },
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := Run(Options{
				Out: &out, Rows: 30, Cols: 100, IsTTY: true,
				Branding: DefaultBranding(),
				Frame:    time.Millisecond, Now: fixedClock(33 * time.Millisecond),
				MaxFrames: 3, Seed: 7, Size: size,
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			got := out.String()
			if n := strings.Count(got, seqEnter); n != 1 {
				t.Errorf("entered the alternate screen %d times, want 1: the buffer was rebuilt for nothing", n)
			}
			if !strings.ContainsRune(got, '█') {
				t.Error("stopped painting the wordmark")
			}
		})
	}
}

// A nil Size is how every caller that has no console to measure runs, the
// tests above included, and it has to mean "keep what you were given".
func TestNextSize(t *testing.T) {
	for _, tc := range []struct {
		name         string
		size         func() (int, int)
		wantR, wantC int
		wantChanged  bool
	}{
		{"nil", nil, 24, 80, false},
		{"same", func() (int, int) { return 24, 80 }, 24, 80, false},
		{"zero", func() (int, int) { return 0, 0 }, 24, 80, false},
		{"one dimension missing", func() (int, int) { return 50, 0 }, 24, 80, false},
		{"grew", func() (int, int) { return 50, 160 }, 50, 160, true},
		{"shrank", func() (int, int) { return 10, 40 }, 10, 40, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, c, changed := nextSize(tc.size, 24, 80)
			if r != tc.wantR || c != tc.wantC || changed != tc.wantChanged {
				t.Errorf("nextSize = %d, %d, %v; want %d, %d, %v", r, c, changed, tc.wantR, tc.wantC, tc.wantChanged)
			}
		})
	}
}

// escThenEOF delivers one Escape press and then ends the input. With a frame
// interval no test will wait for, those two reads are the only things that
// advance the loop, so the frames land in a fixed order.
type escThenEOF struct{ sent bool }

func (r *escThenEOF) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	p[0] = esc
	return 1, nil
}

// Escape hands the console to the streaming kernel log. Measuring it there and
// rebuilding the buffer would clear the screen, wiping the log the user
// pressed Escape to read, so the poll waits until the animation has the
// console back.
func TestRunDoesNotResizeWhileTheKernelLogIsOnScreen(t *testing.T) {
	var out bytes.Buffer
	polls := 0
	err := Run(Options{
		Out: &out, In: &escThenEOF{}, Console: &fakeConsole{},
		Rows: 25, Cols: 80, IsTTY: true, Branding: DefaultBranding(),
		Frame: time.Hour, Now: fixedClock(33 * time.Millisecond),
		MaxFrames: 2, Seed: 7,
		Size: func() (int, int) {
			polls++
			if polls == 1 {
				return 25, 80
			}
			return 50, 160
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if polls != 1 {
		t.Errorf("measured the console %d times, want 1: it was measured with the kernel log on screen", polls)
	}
	if n := strings.Count(out.String(), seqEnter); n != 1 {
		t.Errorf("entered the alternate screen %d times, want 1: the rebuild cleared the log view", n)
	}
}
