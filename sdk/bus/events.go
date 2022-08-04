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
)

type InstallPayload struct {
	Token  string `json:"token"`
	Config string `json:"config"`
}

type BootstrapPayload struct {
	APIAddress string `json:"api"`
	Config     string `json:"config"`
	Logfile    string `json:"logfile"`
}

type EventPayload struct {
	Config string `json:"config"`
}
