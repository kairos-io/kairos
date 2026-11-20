package internal

import "github.com/kairos-io/kairos/v4/sdk/types/logger"

var Log logger.KairosLogger

// The init function initializes the default logger for the package.
// This ensures that logging is configured and ready for use throughout the package.
func init() {
	Log = logger.NewKairosLogger("aurora", "info", false)
}
