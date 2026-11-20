package store

import (
	"context"
	"encoding/json"
	"time"
)

// UpgradeOutcome maps the boot state a node reports after an upgrade reboot to
// the upgrade state it settles in:
//   - active           → done (the node came back up on the upgraded image)
//   - passive/recovery → failed (the upgraded active image did not boot and the
//     node fell back)
//
// ok is false for any other boot state, which leaves the upgrade pending.
func UpgradeOutcome(bootState string) (state string, ok bool) {
	switch bootState {
	case BootStateActive:
		return UpgradeStateDone, true
	case BootStatePassive, BootStateRecovery:
		return UpgradeStateFailed, true
	}
	return "", false
}

// ResetOutcome maps the boot state a node reports after a reset reboot to the
// reset state it moves to from current (kairos-io/kairos#4255):
//   - autoreset        → in-progress, from pending only (the node booted the
//     statereset entry and is mid-reset; the following boot will be active)
//   - active           → done (the reset finished and the node is healthy again)
//   - passive/recovery → failed (the active image did not survive the reset)
//
// ok is false when the boot state moves nothing from current.
func ResetOutcome(current, bootState string) (state string, ok bool) {
	switch bootState {
	case BootStateAutoReset:
		if current == ResetStatePending {
			return ResetStateInProgress, true
		}
	case BootStateActive:
		return ResetStateDone, true
	case BootStatePassive, BootStateRecovery:
		return ResetStateFailed, true
	}
	return "", false
}

// ResolveReset advances a node's reset lifecycle from the boot state it
// reports when it registers again, which it does after a reset that cleared
// its credentials. Transitions are compare-and-set so concurrent reports
// resolve exactly once. Best-effort: a store error is ignored and the next
// report retries. A node not awaiting a reset is left untouched.
func ResolveReset(ctx context.Context, nodes NodeStore, node *ManagedNode, bootState string) {
	if node.ResetState != ResetStatePending && node.ResetState != ResetStateInProgress {
		return
	}
	to, ok := ResetOutcome(node.ResetState, bootState)
	if !ok {
		return
	}
	from := []string{ResetStatePending, ResetStateInProgress}
	if to == ResetStateInProgress {
		from = []string{ResetStatePending}
	}
	_, _ = nodes.AdvanceReset(ctx, node.ID, from, to, to == ResetStateDone)
}

// ResolveUpgrade advances a node's upgrade lifecycle from the boot state it
// reports when it registers again. Transitions are compare-and-set so
// concurrent reports resolve exactly once. Best-effort: a store error is
// ignored and the next report retries. A node not awaiting an upgrade is left
// untouched.
func ResolveUpgrade(ctx context.Context, nodes NodeStore, node *ManagedNode, bootState string) {
	if node.UpgradeState != UpgradeStatePending {
		return
	}
	to, ok := UpgradeOutcome(bootState)
	if !ok {
		return
	}
	_, _ = nodes.AdvanceUpgrade(ctx, node.ID, []string{UpgradeStatePending}, to, to == UpgradeStateDone)
}

// BootedAt returns when a node booted, from the uptime in a heartbeat's metrics
// sample, and false when the sample carries no uptime. An uptime of 0 is a node
// that has just booted, so presence is checked rather than the value.
func BootedAt(now time.Time, metrics json.RawMessage) (time.Time, bool) {
	if len(metrics) == 0 {
		return time.Time{}, false
	}
	var m struct {
		UptimeSeconds *uint64 `json:"uptimeSeconds"`
	}
	if err := json.Unmarshal(metrics, &m); err != nil || m.UptimeSeconds == nil {
		return time.Time{}, false
	}
	return now.Add(-time.Duration(*m.UptimeSeconds) * time.Second), true
}
