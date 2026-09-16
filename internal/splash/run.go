package splash

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

// Terminal control sequences. Kept together so the enter/leave pair is
// obviously symmetric: whatever enter turns on, leave turns back off.
const (
	seqEnter = "\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H" // alt screen, hide cursor, clear
	seqLeave = "\x1b[0m\x1b[?25h\x1b[?1049l"       // reset, show cursor, main screen
)

// Frame rate and animation constants.
const (
	// DefaultFrame is 30fps. The virtual console can sustain this for a
	// diffed repaint of a few hundred cells.
	DefaultFrame = 33 * time.Millisecond
	numParticles = 5
	trailLife    = 5
)

// Options configures Run. Everything the animation touches outside its own
// buffer is a field here, so a test drives the whole loop with no tty, no
// kernel and no clock.
type Options struct {
	// Out is the console. Required.
	Out io.Writer
	// In is the console input, polled for the ESC toggle. A nil In disables
	// the toggle, which is what a splash with no keyboard should do.
	In io.Reader
	// Rows and Cols are the console size in cells.
	Rows, Cols int
	// IsTTY is false when Out is not a terminal, which selects the
	// single-line fallback instead of the animation.
	IsTTY bool
	// Branding is the artwork to paint.
	Branding Branding
	// Version is shown under the tagline. Empty omits the line.
	Version string
	// Console handles the ESC transitions. A nil Console disables the
	// toggle even when In is set.
	Console Console
	// Frame is the time between frames. Zero means DefaultFrame.
	Frame time.Duration
	// Now defaults to time.Now. Injected so a test gets deterministic
	// frames instead of whatever the wall clock happened to do.
	Now func() time.Time
	// Done ends the animation. This is the SIGTERM at switch-root; a nil
	// Done with MaxFrames unset would animate forever, which is exactly
	// what a boot splash should do until something stops it.
	Done <-chan struct{}
	// MaxFrames stops after that many frames. Zero means unlimited. Only
	// tests set this.
	MaxFrames int
	// Duration stops the animation after that much elapsed time. Zero means
	// unlimited, which is what the initramfs unit wants: it animates until
	// switch-root sends SIGTERM. The booted-system unit is a oneshot ordered
	// before getty.target, so there it has to end on its own or the login
	// prompt never arrives.
	Duration time.Duration
	// Seed makes the particle orbits reproducible. Zero derives one from
	// the clock.
	Seed uint32
}

// ErrNoBranding reports that the branding directory is absent, which is how a
// distribution opts out of the splash.
var ErrNoBranding = errors.New("splash: no branding directory")

// Run paints the splash until Done fires, Duration elapses, MaxFrames is
// reached, or the console turns out not to be usable.
//
// It never returns an error for a console it cannot animate on. A boot splash
// that fails loudly is worse than no splash: the unit is wanted rather than
// required precisely so that this path cannot stop a boot.
func Run(o Options) error {
	if o.Out == nil {
		return errors.New("splash: no output")
	}
	if o.Branding.Height() == 0 {
		o.Branding = DefaultBranding()
	}
	if !o.IsTTY || !o.Branding.Fits(o.Rows, o.Cols) {
		return writeFallback(o.Out, o.Branding, o.Version)
	}
	if o.Frame <= 0 {
		o.Frame = DefaultFrame
	}
	if o.Now == nil {
		o.Now = time.Now
	}

	start := o.Now()
	seed := o.Seed
	if seed == 0 {
		seed = uint32(start.UnixNano())
	}

	g := newGrid(o.Out, o.Rows, o.Cols)
	parts := newParticles(o.Rows, o.Cols, o.Branding, seed)

	var m *machine
	keys := make(chan []byte, 8)
	if o.In != nil && o.Console != nil {
		m = newMachine(o.Console)
		go readKeys(o.In, keys)
	} else {
		// A nil channel blocks in select, which is what "no toggle" means.
		keys = nil
	}

	_, _ = io.WriteString(o.Out, seqEnter)
	defer func() { _, _ = io.WriteString(o.Out, seqLeave) }()

	tick := time.NewTicker(o.Frame)
	defer tick.Stop()

	prev := start
	for frame := 0; o.MaxFrames == 0 || frame < o.MaxFrames; frame++ {
		now := o.Now()
		if o.Duration > 0 && now.Sub(start) >= o.Duration {
			return nil
		}
		dt := now.Sub(prev).Seconds()
		prev = now

		if m == nil || m.Mode() == ModeSplash {
			paintFrame(g, o.Branding, o.Version, parts, now.Sub(start).Seconds(), dt)
			if err := g.Flush(); err != nil {
				// The console went away mid-boot. Nothing to salvage and
				// nothing worth failing a boot over.
				return nil
			}
		}

		select {
		case <-o.Done:
			return nil
		case p, ok := <-keys:
			if !ok {
				keys = nil
				continue
			}
			if m.Feed(p) && m.Mode() == ModeSplash {
				// The log view scrolled over the buffer, so the diff is
				// no longer a truthful picture of the screen.
				_, _ = io.WriteString(o.Out, seqEnter)
				g.invalidate()
			}
		case <-tick.C:
		}
	}
	return nil
}

// paintFrame draws one frame: the decayed trails, then the wordmark, tagline
// and version on top, then the particles over everything.
func paintFrame(g *grid, b Branding, version string, parts []particle, t, dt float64) {
	g.decay()
	g.paintTrails()

	blockH := b.Height() + 3
	top := (g.rows - blockH) / 2
	left := (g.cols - b.Width()) / 2

	// The ramp advances a little under once a second, slow enough to read as
	// a colour breath rather than a flicker.
	phase := int(t * 0.7)
	pal := b.Palette
	if len(pal) == 0 {
		pal = kairosPalette
	}
	for i, row := range b.Wordmark {
		g.paintRunes(top+i, left, row, pal[phase%len(pal)])
	}
	g.paintCentered(top+b.Height()+1, b.Tagline, pal[(phase+2)%len(pal)])
	if version != "" {
		g.paintCentered(top+b.Height()+2, version, 93)
	}

	for i := range parts {
		parts[i].theta += parts[i].omega * dt
		x := int(parts[i].cx + parts[i].rx*math.Cos(parts[i].theta) + 0.5)
		y := int(parts[i].cy - parts[i].ry*math.Sin(parts[i].theta) + 0.5)
		g.set(y, x, "o", pal[(parts[i].phase+int(t*1.5))%len(pal)], trailLife)
	}
}

// Fits reports whether the branding can be drawn on a console of that size.
// The row budget is the wordmark plus a tagline, a version line and a margin.
func (b Branding) Fits(rows, cols int) bool {
	return rows >= b.Height()+6 && cols >= b.Width()+2
}

// writeFallback prints the one line a console that cannot animate should get:
// a serial console, a kernel without CONFIG_VT, or a window too small for the
// wordmark. Still worth printing, because it is the only confirmation that the
// splash ran at all.
func writeFallback(w io.Writer, b Branding, version string) error {
	name := b.Name
	if name == "" {
		name = "KAIROS"
	}
	var err error
	if version != "" {
		_, err = fmt.Fprintf(w, "%s  %s\n", name, version)
	} else {
		_, err = fmt.Fprintf(w, "%s\n", name)
	}
	return err
}

// readKeys forwards console input to the loop and closes the channel when the
// input ends, which tells the loop to stop selecting on it.
func readKeys(r io.Reader, out chan<- []byte) {
	defer close(out)
	buf := make([]byte, 32)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			p := make([]byte, n)
			copy(p, buf[:n])
			select {
			case out <- p:
			default: // the loop is busy; dropping a frame of input is fine
			}
		}
		if err != nil {
			return
		}
	}
}

// particle is one orbiting dot.
type particle struct {
	cx, cy, rx, ry, theta, omega float64
	phase                        int
}

// newParticles seeds orbits that clear the wordmark block, so the dots circle
// the logo instead of crossing it.
func newParticles(rows, cols int, b Branding, seed uint32) []particle {
	s := seed
	if s == 0 {
		s = 1
	}
	cx, cy := float64(cols)/2, float64(rows)/2
	brx := float64(b.Width())/2 + 4
	bry := float64(b.Height())/2 + 3
	parts := make([]particle, numParticles)
	for i := range parts {
		parts[i] = particle{
			cx:    cx,
			cy:    cy,
			rx:    brx + nextFloat(&s)*4,
			ry:    bry + nextFloat(&s)*2,
			theta: nextFloat(&s) * 2 * math.Pi,
			omega: 0.8 + nextFloat(&s)*1.4,
			phase: int(nextUint(&s) % 7),
		}
	}
	return parts
}

// nextUint is xorshift32. The splash needs orbits that differ between boots,
// not randomness anyone should rely on, and a three-line generator keeps the
// frame loop free of allocation and of a crypto dependency.
func nextUint(s *uint32) uint32 {
	x := *s
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	if x == 0 {
		x = 0xdeadbeef
	}
	*s = x
	return x
}

func nextFloat(s *uint32) float64 {
	return float64(nextUint(s)&0xFFFFFF) / float64(0x1000000)
}

// ReadOSVersion returns the "version: <VERSION_ID>" line for the splash, or
// the empty string when /etc/os-release has no VERSION_ID. The path is a
// parameter so a test does not need to write to /etc.
func ReadOSVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		v, ok := strings.CutPrefix(line, "VERSION_ID=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`))
		if v == "" {
			return ""
		}
		if len(v) > 40 {
			v = v[:40]
		}
		return "version: " + v
	}
	return ""
}
