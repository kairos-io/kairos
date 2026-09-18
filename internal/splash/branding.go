// Package splash draws the Kairos boot splash on a Linux virtual console.
//
// The animation is data-driven: the wordmark, the tagline and the colour ramp
// come from a branding directory on disk, so a downstream distribution ships
// its own artwork without forking this code. Everything has a built-in Kairos
// default, so an absent or partial branding directory still animates.
package splash

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DefaultBrandingDir is where a distribution drops its artwork. It is also the
// switch that turns the splash on: the dracut module and the systemd unit both
// gate on this directory existing, because gating on the binary is not an
// option (the binary is the whole multi-call kairos tool).
const DefaultBrandingDir = "/etc/kairos/branding/splash"

// Branding is the artwork the animation paints.
type Branding struct {
	// Wordmark is the block-art logo, one string per row. All rows have the
	// same display width; Load rejects artwork where they do not.
	Wordmark []string
	// Tagline is a single centred line under the wordmark. It is dropped
	// rather than truncated when the console is too narrow for it.
	Tagline string
	// Palette is the colour ramp the wordmark cycles through, as SGR
	// foreground codes. Only VGA-16 codes are allowed; see ValidColor.
	Palette []int
	// Name is the plain-text fallback printed when there is no usable
	// terminal, so a serial-only boot still gets one identifying line.
	Name string
}

// kairosWordmark is "KAIROS" in the figlet ANSI Shadow font, the same face the
// hadron splash uses, so the two distributions look like one family.
var kairosWordmark = []string{
	"██╗  ██╗ █████╗ ██╗██████╗  ██████╗ ███████╗",
	"██║ ██╔╝██╔══██╗██║██╔══██╗██╔═══██╗██╔════╝",
	"█████╔╝ ███████║██║██████╔╝██║   ██║███████╗",
	"██╔═██╗ ██╔══██║██║██╔══██╗██║   ██║╚════██║",
	"██║  ██╗██║  ██║██║██║  ██║╚██████╔╝███████║",
	"╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝  ╚═╝ ╚═════╝ ╚══════╝",
}

// kairosPalette is a blue-to-magenta ramp in VGA-16. The kernel virtual
// console has 16 colours; 256-colour codes render as the wrong hue there even
// though they look right in a graphical terminal emulator.
var kairosPalette = []int{94, 96, 36, 34, 95, 97}

// DefaultBranding returns the built-in Kairos artwork.
func DefaultBranding() Branding {
	return Branding{
		Wordmark: append([]string(nil), kairosWordmark...),
		Tagline:  "The immutable Linux meta-distribution for edge Kubernetes",
		Palette:  append([]int(nil), kairosPalette...),
		Name:     "KAIROS",
	}
}

// ValidColor reports whether c is one of the 16 foreground SGR codes a Linux
// virtual console can actually render: 30-37 and their bright counterparts
// 90-97.
func ValidColor(c int) bool {
	return (c >= 30 && c <= 37) || (c >= 90 && c <= 97)
}

// Width returns the display width of the wordmark in cells, counting runes
// rather than bytes: the block-art glyphs are multi-byte and every one of them
// occupies a single console cell.
func (b Branding) Width() int {
	w := 0
	for _, row := range b.Wordmark {
		if n := utf8.RuneCountInString(row); n > w {
			w = n
		}
	}
	return w
}

// Height returns the number of wordmark rows.
func (b Branding) Height() int { return len(b.Wordmark) }

// LoadBranding reads artwork from dir, falling back to the Kairos default for
// every part that is missing or unusable. It returns the branding it will
// actually paint plus one error per rejected part, so a caller can log why a
// customisation did not take effect while still showing something.
//
// A missing directory is not an error: it is the normal case on an image with
// no branding installed, and it yields the default with no errors at all.
//
// The files, all optional:
//
//	wordmark   block art, one row per line, every row the same display width
//	tagline    one line of text
//	palette    SGR foreground codes, one per line or space separated
//	name       the plain-text fallback string
func LoadBranding(dir string) (Branding, []error) {
	b := DefaultBranding()
	if dir == "" {
		return b, nil
	}
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return b, nil
		}
		return b, []error{fmt.Errorf("branding dir %s: %w", dir, err)}
	}

	var errs []error
	read := func(name string) (string, bool) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
			return "", false
		}
		return string(raw), true
	}

	if s, ok := read("wordmark"); ok {
		if rows, err := parseWordmark(s); err != nil {
			errs = append(errs, fmt.Errorf("wordmark: %w", err))
		} else {
			b.Wordmark = rows
		}
	}
	if s, ok := read("tagline"); ok {
		// A tagline file that exists but is blank means "no tagline", which
		// is a legitimate choice and not an error.
		b.Tagline = strings.TrimSpace(firstLine(s))
	}
	if s, ok := read("palette"); ok {
		if pal, err := parsePalette(s); err != nil {
			errs = append(errs, fmt.Errorf("palette: %w", err))
		} else {
			b.Palette = pal
		}
	}
	if s, ok := read("name"); ok {
		if n := strings.TrimSpace(firstLine(s)); n != "" {
			b.Name = n
		} else {
			errs = append(errs, errors.New("name: empty"))
		}
	}
	return b, errs
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// parseWordmark splits block art into rows and insists every row is the same
// display width. Ragged art is rejected rather than padded: the painter
// centres the block by its width, so one long row silently shifts every other
// row off-centre, which looks like a rendering bug rather than bad input.
func parseWordmark(s string) ([]string, error) {
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var rows []string
	for sc.Scan() {
		rows = append(rows, strings.TrimRight(sc.Text(), "\r"))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// Trailing blank lines are what a text editor leaves behind, not art.
	for len(rows) > 0 && strings.TrimSpace(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) == 0 {
		return nil, errors.New("no rows")
	}
	want := utf8.RuneCountInString(rows[0])
	for i, r := range rows {
		if !utf8.ValidString(r) {
			return nil, fmt.Errorf("row %d is not valid UTF-8", i+1)
		}
		if got := utf8.RuneCountInString(r); got != want {
			return nil, fmt.Errorf("row %d is %d cells wide, row 1 is %d", i+1, got, want)
		}
	}
	return rows, nil
}

// parsePalette reads SGR foreground codes and rejects anything outside VGA-16.
// A palette is all-or-nothing: one bad entry falls the whole ramp back to the
// default rather than leaving a gap that would paint an unstyled frame.
func parsePalette(s string) ([]int, error) {
	var pal []int
	for _, f := range strings.Fields(s) {
		// Allow a trailing comma so a copied Go or JSON list works.
		f = strings.TrimSuffix(f, ",")
		if f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		if !ValidColor(n) {
			return nil, fmt.Errorf("%d is not a VGA-16 foreground code (want 30-37 or 90-97)", n)
		}
		pal = append(pal, n)
	}
	if len(pal) == 0 {
		return nil, errors.New("no colours")
	}
	return pal, nil
}
