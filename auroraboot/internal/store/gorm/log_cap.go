package gorm

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxLogChars caps the log text kept on one artifact or extension record, so
// a chatty or runaway build cannot grow a row without bound.
const maxLogChars = 4 * 1024 * 1024

// logCap is the cap cappedLogAppend enforces. It is maxLogChars; tests lower
// it to exercise truncation with short strings.
var logCap = maxLogChars

// logTruncatedMarker heads a stored log whose oldest text was dropped.
const logTruncatedMarker = "[earlier log output truncated]\n"

// cappedLogAppend returns the new value of the logs column after appending
// chunk, as one expression so the append stays a single atomic UPDATE. When
// the result would pass logCap characters it keeps the newest logCap of them
// behind logTruncatedMarker. A capped log is the marker plus logCap
// characters, so any later append pushes the old marker out of the kept tail
// and markers never stack. The SQL sticks to length, substr with a positive
// start and ||, which SQLite and Postgres treat alike. NUL bytes are dropped
// first: SQLite's length stops counting at the first one, which would let the
// row grow past the cap, and Postgres refuses them in text.
func cappedLogAppend(chunk string) clause.Expr {
	chunk = strings.ReplaceAll(chunk, "\x00", "")
	appended := "COALESCE(logs, '') || ?"
	sql := fmt.Sprintf("CASE WHEN length(%[1]s) > %[2]d THEN ? || substr(%[1]s, length(%[1]s) - %[2]d + 1) ELSE %[1]s END",
		appended, logCap)
	return gorm.Expr(sql, chunk, logTruncatedMarker, chunk, chunk, chunk)
}
