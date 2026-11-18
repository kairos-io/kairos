package handlers

import (
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// resetAnchor returns the timestamp the reset timeout is measured from: the
// later of when the reset was requested and when the node last proved it came
// back from the reset reboot. Nil when no reset has been requested.
func resetAnchor(node *store.ManagedNode) *time.Time {
	anchor := node.ResetRequestedAt
	if node.ResetProgressAt != nil && (anchor == nil || node.ResetProgressAt.After(*anchor)) {
		anchor = node.ResetProgressAt
	}
	return anchor
}

// resetExpired reports whether an in-flight reset has reached its deadline.
// A negative timeout disables expiration; zero is normalized by the handler's
// configuration before this helper is called.
func resetExpired(node *store.ManagedNode, timeout time.Duration, now time.Time) bool {
	if timeout < 0 {
		return false
	}
	if node.ResetState != store.ResetStatePending && node.ResetState != store.ResetStateInProgress {
		return false
	}
	anchor := resetAnchor(node)
	if anchor == nil {
		return false
	}
	return !now.Before(anchor.Add(timeout))
}
