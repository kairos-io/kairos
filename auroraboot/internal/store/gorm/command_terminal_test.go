package gorm_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// A command that reached a terminal phase is history. A late or replayed
// status report must not move it back to Running or rewrite its result.
var _ = Describe("Gorm Store terminal command guard", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
		n   *store.ManagedNode
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "terminal.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
		n = &store.ManagedNode{MachineID: "m1"}
		Expect(s.Register(ctx, n)).To(Succeed())
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	issue := func() string {
		cmd := &store.NodeCommand{ManagedNodeID: n.ID, Command: "exec"}
		Expect(s.CreateBatch(ctx, []*store.NodeCommand{cmd})).To(Succeed())
		return cmd.ID
	}

	get := func(id string) *store.NodeCommand {
		cmds, err := s.ListByNode(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		for _, c := range cmds {
			if c.ID == id {
				return c
			}
		}
		Fail("command " + id + " not found")
		return nil
	}

	reporters := []struct {
		name   string
		report func(id, phase, result string) error
	}{
		{"UpdateStatus", func(id, phase, result string) error {
			return s.UpdateStatus(ctx, id, phase, result)
		}},
		{"UpdateStatusForNode", func(id, phase, result string) error {
			return s.UpdateStatusForNode(ctx, id, n.ID, phase, result)
		}},
	}

	for _, r := range reporters {
		report := r.report
		Describe(r.name, func() {
			for _, terminal := range store.TerminalCommandPhases {
				terminal := terminal

				It("refuses to move a "+terminal+" command back to Running", func() {
					id := issue()
					Expect(report(id, terminal, "first")).To(Succeed())

					Expect(report(id, store.CommandRunning, "later")).To(MatchError(store.ErrCommandTerminal))

					got := get(id)
					Expect(got.Phase).To(Equal(terminal))
					Expect(got.Result).To(Equal("first"))
				})

				It("treats a repeated "+terminal+" report as a no-op and keeps the first result", func() {
					id := issue()
					Expect(report(id, terminal, "first")).To(Succeed())

					Expect(report(id, terminal, "second")).To(Succeed())

					got := get(id)
					Expect(got.Phase).To(Equal(terminal))
					Expect(got.Result).To(Equal("first"))
				})
			}

			It("refuses to flip a Completed command to Failed", func() {
				id := issue()
				Expect(report(id, store.CommandCompleted, "ok")).To(Succeed())
				Expect(report(id, store.CommandFailed, "boom")).To(MatchError(store.ErrCommandTerminal))
				Expect(get(id).Phase).To(Equal(store.CommandCompleted))
			})

			It("still moves a live command through Running to a terminal phase", func() {
				id := issue()
				Expect(report(id, store.CommandRunning, "")).To(Succeed())
				Expect(report(id, store.CommandCompleted, "ok")).To(Succeed())
				Expect(get(id).Phase).To(Equal(store.CommandCompleted))
			})

			It("still reports a missing command as not found", func() {
				Expect(report("missing", store.CommandRunning, "")).To(MatchError(store.ErrCommandNotFound))
			})
		})
	}

	It("UpdateStatusForNode reports another node's finished command as not found, not terminal", func() {
		id := issue()
		Expect(s.UpdateStatus(ctx, id, store.CommandCompleted, "ok")).To(Succeed())
		other := &store.ManagedNode{MachineID: "m2"}
		Expect(s.Register(ctx, other)).To(Succeed())

		Expect(s.UpdateStatusForNode(ctx, id, other.ID, store.CommandRunning, "")).
			To(MatchError(store.ErrCommandNotFound))
	})
})
