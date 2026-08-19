package phonehome

import (
	"sync"

	"github.com/kairos-io/kairos-sdk/state"
	"github.com/rs/zerolog"
)

// Boot-state values reported to the fleet server, the day-2 lifecycle vocabulary
// agreed on kairos-io/kairos#4253:
//
//	active | passive | recovery | autoreset | livecd | unknown
//
// These are the short fleet names. kairos-sdk/state uses longer constants
// (active_boot, passive_boot, ...); mapBootState translates between them.
const (
	bootStateActive    = "active"
	bootStatePassive   = "passive"
	bootStateRecovery  = "recovery"
	bootStateAutoReset = "autoreset"
	bootStateLiveCD    = "livecd"
	bootStateUnknown   = "unknown"
)

// bootStateNames maps kairos-sdk boot states to the short fleet vocabulary.
// state.Unknown maps to "unknown" — and, crucially, so does any value not in
// this table (via mapBootState's default). A node whose boot state cannot be
// determined must never be reported as "active": that would mask a broken node
// (e.g. one that fell back to the passive image) as healthy.
var bootStateNames = map[state.Boot]string{
	state.Active:    bootStateActive,
	state.Passive:   bootStatePassive,
	state.Recovery:  bootStateRecovery,
	state.AutoReset: bootStateAutoReset,
	state.LiveCD:    bootStateLiveCD,
	state.Unknown:   bootStateUnknown,
}

// mapBootState translates a kairos-sdk boot state to the short fleet name,
// defaulting to "unknown" for anything unrecognised.
func mapBootState(b state.Boot) string {
	if s, ok := bootStateNames[b]; ok {
		return s
	}
	return bootStateUnknown
}

var (
	bootStateOnce   sync.Once
	bootStateCached string
)

// detectBootState returns the node's boot state using the shared, UKI-aware
// kairos-sdk detector (state.DetectBoot inspects /proc/cmdline and, on UKI, the
// EFI current-entry file). The result is cached on first call: a node's boot
// state is constant for the life of a boot, so re-reading it on every heartbeat
// is wasteful.
func detectBootState(logger zerolog.Logger) string {
	bootStateOnce.Do(func() {
		bootStateCached = mapBootState(state.DetectBoot(logger))
	})
	return bootStateCached
}
