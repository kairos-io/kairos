package webui

import (
	"regexp"
	"strings"
)

// csiSequence matches an ANSI CSI escape sequence: ESC [, the parameter and
// intermediate bytes, then the final byte that names the command.
//
// The agent logs through zerolog's console writer, which colours its output
// whenever it is not writing to a file, so every line it puts on stdout is
// wrapped in SGR sequences (`ESC[32m` ... `ESC[0m`). Those are the only
// sequences observed in an install transcript, but the pattern covers CSI as a
// whole rather than SGR alone, so a tool the agent shells out to that moves the
// cursor or erases a line does not put `[2K` on the page either.
var csiSequence = regexp.MustCompile(`\x1b\[[0-9;:?]*[ -/]*[@-~]`)

// stripANSI removes ANSI CSI escape sequences from one line of agent output.
//
// The progress page writes each log line with textContent, which neither
// escapes nor interprets markup: that is deliberate, because the alternative
// the web UI used to run was converting the codes to HTML spans server-side and
// trusting the browser with the result. The cost is that a sequence which
// reaches the browser is rendered as its literal characters, so it has to come
// off here instead.
//
// Indentation is left alone. A log pane is read as a transcript, and the lines
// are rendered in a monospace font, so leading whitespace is information.
func stripANSI(line string) string {
	if !strings.ContainsRune(line, '\x1b') {
		return line
	}
	return csiSequence.ReplaceAllString(line, "")
}
