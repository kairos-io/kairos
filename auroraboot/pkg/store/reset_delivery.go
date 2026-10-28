package store

import (
	"context"
	"log"
)

// MarkResetDelivered re-anchors a node's reset timeout on the moment the node
// took delivery of its reset command.
//
// The reset command and the node-level lifecycle are both given the same
// window at request time, so a node that is offline for almost all of it can
// claim the command just before the deadline and then be reported failed while
// it is rebooting into the wipe. Once that happens the node is stuck: the
// re-register path (store.ResolveReset) only advances a reset that is
// still in flight, so `failed` is never corrected and LastReset is never
// stamped, and a CAPI provider sees a failure for a node that is wiping fine.
//
// Claiming the command is the first proof that the reset reached the node, so
// it restarts the clock exactly as a mid-reset re-register does.
//
// Call this from every path that wins a ClaimForDelivery, right after the
// claim. It is a no-op for any command that is not a reset. Best-effort, like
// the rest of the reset lifecycle: a store error is logged and discarded,
// because failing an agent's command poll over a bookkeeping write would cost
// more than the refreshed budget is worth, and the next delivery or
// re-register stamps it again.
func MarkResetDelivered(ctx context.Context, nodes NodeStore, cmd *NodeCommand) {
	if nodes == nil || cmd == nil || cmd.Command != CmdReset {
		return
	}
	if _, err := nodes.StampResetProgress(ctx, cmd.ManagedNodeID); err != nil {
		log.Printf("reset delivery: node %s: %v", cmd.ManagedNodeID, err)
	}
}
