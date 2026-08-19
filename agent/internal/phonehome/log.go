package phonehome

import (
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
)

// Logger is the package-level logger used by DefaultCommandHandler and
// Uninstall's helpers — anywhere below the Client where plumbing a logger
// parameter through every call site would just be noise.
//
// The default is a standard Kairos logger (journald when the binary is
// running under systemd, /var/log/kairos/phonehome.log + stderr otherwise),
// matching every other subsystem. Embedders that already run inside a
// logger pipeline point us at their existing KairosLogger via SetLogger at
// startup so everything shares one output stream. Tests call
// SetLogger(sdkLogger.NewNullLogger()) to silence output under ginkgo.
var Logger = sdkLogger.NewKairosLogger("phonehome", "info", false)

// SetLogger replaces the package logger. agent.Run calls this from
// enablePhoneHomeIfConfigured with the scanned sdkConfig.Config.Logger so
// the remote command handlers land in the same journal stream as the rest
// of kairos-agent's output.
func SetLogger(l sdkLogger.KairosLogger) {
	Logger = l
}
