package bus

import (
	"github.com/mudler/go-pluggable"
)

const (
	// Package events.

	// EventChallenge is issued before installation begins to gather information about how the device should be provisioned.
	EventChallenge pluggable.EventType = "agent.install.challenge"

	// EventInstall is issues before the os is installed to allow the device to be configured.
	EventInstall pluggable.EventType = "agent.install"

	// EventBoot is issues on every startup, excluding live and recovery mode, in the initramfs stage.
	EventBoot pluggable.EventType = "agent.boot"

	// EventBootstrap is issued to run any initial cluster configuration.
	EventBootstrap pluggable.EventType = "agent.bootstrap"

	// EventInstallPrompt is issued to request which config are required to ask to the user
	EventInstallPrompt pluggable.EventType = "agent.installprompt"

	// EventRecovery emitted while booting into recovery mode
	EventRecovery     pluggable.EventType = "agent.recovery"
	EventRecoveryStop pluggable.EventType = "agent.recovery.stop"

	EventInteractiveInstall pluggable.EventType = "agent.interactive-install"
)

type InstallPayload struct {
	Token  string `json:"token"`
	Config string `json:"config"`
}

type YAMLPrompt struct {
	YAMLSection string
	Bool        bool
	Prompt      string
	Default     string
	AskFirst    bool
	AskPrompt   string
	IfEmpty     string
	PlaceHolder string
}

type BootstrapPayload struct {
	APIAddress string `json:"api"`
	Config     string `json:"config"`
	Logfile    string `json:"logfile"`
}

type EventPayload struct {
	Config string `json:"config"`
}

// AllEvents is a convenience list of all the events streamed from the bus.
var AllEvents = []pluggable.EventType{
	EventBootstrap,
	EventChallenge,
	EventBoot,
	EventInstall,
	EventRecovery,
	EventInteractiveInstall,
	EventRecoveryStop,
}

// IsEventDefined checks wether an event is defined in the bus.
// It accepts strings or EventType, returns a boolean indicating that
// the event was defined among the events emitted by the bus.
func IsEventDefined(i interface{}) bool {
	checkEvent := func(e pluggable.EventType) bool {
		for _, ee := range AllEvents {
			if ee == e {
				return true
			}
		}

		return false
	}

	switch f := i.(type) {
	case string:
		return checkEvent(pluggable.EventType(f))
	case pluggable.EventType:
		return checkEvent(f)
	default:
		return false
	}
}
