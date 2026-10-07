package splash

import (
	"bufio"
	"io"
	"strconv"
)

// cell is one console character position: the glyph, its SGR colour (0 meaning
// unstyled), and a trail intensity that counts down to zero.
type cell struct {
	glyph     string
	color     int
	intensity uint8
}

// grid is a double-buffered console. Paint into it, then Flush writes only the
// cells that changed since the last Flush. The virtual console is slow enough
// that redrawing every cell every frame is visible as tearing.
type grid struct {
	rows, cols int
	cur, prev  []cell
	w          *bufio.Writer
	sgr        int // colour currently set on the terminal, -1 = unknown
}

func newGrid(out io.Writer, rows, cols int) *grid {
	g := &grid{
		rows: rows,
		cols: cols,
		cur:  make([]cell, rows*cols),
		prev: make([]cell, rows*cols),
		w:    bufio.NewWriterSize(out, 16*1024),
		sgr:  -1,
	}
	for i := range g.cur {
		g.cur[i].glyph = " "
		// prev starts as the empty string rather than " ", so the first
		// Flush writes every cell and the terminal matches the buffer even
		// if something else painted over it first.
		g.prev[i].glyph = ""
	}
	return g
}

func (g *grid) set(row, col int, glyph string, color int, intensity uint8) {
	if row < 0 || row >= g.rows || col < 0 || col >= g.cols {
		return
	}
	c := &g.cur[row*g.cols+col]
	c.glyph, c.color, c.intensity = glyph, color, intensity
}

// decay ages every trail cell by one frame.
func (g *grid) decay() {
	for i := range g.cur {
		if g.cur[i].intensity > 0 {
			g.cur[i].intensity--
		}
	}
}

// trailGlyphs is the fade ramp a moving particle leaves behind, indexed by
// intensity. Index 0 is unused: an intensity of 0 clears the cell.
var trailGlyphs = [...]string{" ", ".", "'", ":", "*", "o"}

// paintTrails rewrites every decaying cell to the glyph for its current
// intensity, and clears the ones that have aged out.
func (g *grid) paintTrails() {
	for i := range g.cur {
		c := &g.cur[i]
		switch {
		case c.intensity == 0:
			c.glyph, c.color = " ", 0
		case int(c.intensity) < len(trailGlyphs):
			c.glyph = trailGlyphs[c.intensity]
		}
	}
}

// paintRunes writes a string left to right starting at (row, col), skipping
// spaces so the wordmark does not erase the trails passing behind it.
func (g *grid) paintRunes(row, col int, s string, color int) {
	i := col
	for _, r := range s {
		if r != ' ' {
			g.set(row, i, string(r), color, 0)
		}
		i++
	}
}

// paintCentered writes s centred on the row, and reports whether it fit. A
// line that does not fit is not painted at all: a half-drawn tagline reads as
// a bug, an absent one reads as a narrow console.
func (g *grid) paintCentered(row int, s string, color int) bool {
	if s == "" {
		return true
	}
	n := 0
	for range s {
		n++
	}
	if n > g.cols {
		return false
	}
	g.paintRunes(row, (g.cols-n)/2, s, color)
	return true
}

// Flush writes the cells that changed. It moves the cursor once per run of
// changed cells rather than once per cell, and only emits an SGR sequence when
// the colour differs from what the terminal is already set to.
//
// Runs never cross a row boundary, even though the buffer is one flat slice
// and a terminal with autowrap would put the next character in the right
// place. Writing the bottom-right cell with autowrap on scrolls the screen by
// a line, which would drag the whole animation upwards on the first frame.
func (g *grid) Flush() error {
	for row := 0; row < g.rows; row++ {
		lastCol := -2
		for col := 0; col < g.cols; col++ {
			i := row*g.cols + col
			if g.cur[i] == g.prev[i] {
				continue
			}
			if col != lastCol+1 {
				_, _ = g.w.WriteString("\x1b[")
				_, _ = g.w.WriteString(strconv.Itoa(row + 1))
				_ = g.w.WriteByte(';')
				_, _ = g.w.WriteString(strconv.Itoa(col + 1))
				_ = g.w.WriteByte('H')
			}
			if c := g.cur[i].color; c != g.sgr {
				if c == 0 {
					_, _ = g.w.WriteString("\x1b[0m")
				} else {
					_, _ = g.w.WriteString("\x1b[0;1;")
					_, _ = g.w.WriteString(strconv.Itoa(c))
					_ = g.w.WriteByte('m')
				}
				g.sgr = c
			}
			if gl := g.cur[i].glyph; gl != "" {
				_, _ = g.w.WriteString(gl)
			} else {
				_ = g.w.WriteByte(' ')
			}
			g.prev[i] = g.cur[i]
			lastCol = col
		}
	}
	return g.w.Flush()
}

// invalidate forgets what the terminal is showing, so the next Flush repaints
// every cell. Needed after the log view has scrolled over the animation.
func (g *grid) invalidate() {
	for i := range g.prev {
		g.prev[i] = cell{}
	}
	g.sgr = -1
}
