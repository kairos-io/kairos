//go:build !windows

package logger

import (
	"io"
	"net"

	"github.com/rs/zerolog/journald"
)

func isJournaldAvailable() bool {
	conn, err := net.Dial("unixgram", "/run/systemd/journal/socket")
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}

func getJournaldWriter() io.Writer {
	if !isJournaldAvailable() {
		return nil
	}
	return journald.NewJournalDWriter()
}
