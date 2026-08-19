package utils

import (
	"os"

	"github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/kairos-sdk/types/logger"
)

// KLog is the generic KairosLogger that we pass to kcrypt calls.
var KLog logger.KairosLogger

func SetLogger() {
	level := "info"

	// Set debug level
	debug := len(ReadCMDLineArg("rd.immucore.debug")) > 0
	debugFromEnv := os.Getenv("IMMUCORE_DEBUG") != ""
	if debug || debugFromEnv {
		level = "debug"
	}
	_ = os.MkdirAll(constants.LogDir, os.ModeDir|os.ModePerm)

	KLog = logger.NewKairosLoggerWithExtraDirs("immucore", level, false, constants.LogDir)
}
