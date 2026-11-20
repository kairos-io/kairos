package gorm

import "gorm.io/gorm"

// UnsafeDB exposes the GORM handle for tests that need to drive the
// database directly. Production code must use the store interfaces.
func (s *Store) UnsafeDB() *gorm.DB { return s.db }

// LogTruncatedMarker is the line that heads a log whose start was dropped.
const LogTruncatedMarker = logTruncatedMarker

// MaxLogChars is the production cap on stored log text.
const MaxLogChars = maxLogChars

// SetLogCap sets the cap the log appends enforce and returns a function that
// restores the production cap, so specs can exercise truncation with short
// strings.
func SetLogCap(n int) (restore func()) {
	logCap = n
	return func() { logCap = maxLogChars }
}

// RebootTimeout is the deadline length a batch gives a rebooting node.
var RebootTimeout = rebootTimeout
