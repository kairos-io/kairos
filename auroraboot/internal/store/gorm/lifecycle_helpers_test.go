package gorm_test

import (
	"context"

	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// issueCommand queues one Pending command for nodeID the way the command
// handlers do, so a reset or upgrade also marks the node pending, and returns
// the id the store minted for it.
func issueCommand(ctx context.Context, s *gormstore.Store, nodeID, command string) string {
	cmd := &store.NodeCommand{ManagedNodeID: nodeID, Command: command}
	Expect(s.CreateBatch(ctx, []*store.NodeCommand{cmd})).To(Succeed())
	return cmd.ID
}
