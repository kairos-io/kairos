package auroraboot

import (
	"io"
	"time"

	sdklogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/rs/zerolog"
)

// teeKairosLogger returns a KairosLogger that formats each event through a
// zerolog ConsoleWriter, then writes the rendered bytes to both the base
// logger's stdout console (colours preserved, driven by IsTerminal) and to
// sink. The returned logger's level tracks base's level, but sink only gets
// info and above: it is the build log, stored and streamed to every UI
// client, and debug events carry whole documents such as the cloud-config.
// Any non-console
// outputs the base logger carried (journald, log files) are intentionally
// dropped because the deployer runs per-build - the caller decides which
// destinations that build's progress should reach, and stdout + sink
// covers the two we care about here.
func teeKairosLogger(base sdklogger.KairosLogger, sink io.Writer) sdklogger.KairosLogger {
	writers := zerolog.MultiLevelWriter(
		zerolog.NewConsoleWriter(func(w *zerolog.ConsoleWriter) {
			w.TimeFormat = time.RFC3339
			w.FieldsExclude = []string{"SYSLOG_IDENTIFIER"}
		}),
		&zerolog.FilteredLevelWriter{
			Writer: zerolog.LevelWriterAdapter{Writer: zerolog.ConsoleWriter{
				Out:           sink,
				TimeFormat:    time.RFC3339,
				NoColor:       true,
				FieldsExclude: []string{"SYSLOG_IDENTIFIER"},
			}},
			Level: zerolog.InfoLevel,
		},
	)
	tee := zerolog.New(writers).
		With().
		Str("SYSLOG_IDENTIFIER", "kairos-aurora").
		Timestamp().
		Logger().
		Level(base.Logger.GetLevel())
	return sdklogger.KairosLogger{Logger: tee}
}
