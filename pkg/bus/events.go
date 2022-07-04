package bus

import (
	"github.com/mudler/go-pluggable"
)

var (
	// Package events

	// EventPackageInstall is the event fired when a new package is being installed
	EventBootstrap pluggable.EventType = "agent.bootstrap"
	EventInstall   pluggable.EventType = "agent.install"
	EventChallenge pluggable.EventType = "agent.install.challenge"
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
