//go:build windows

package logger

import "io"

func isJournaldAvailable() bool {
	return false
}

func getJournaldWriter() io.Writer {
	return nil // No journald on Windows
}
