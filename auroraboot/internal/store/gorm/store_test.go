package gorm_test

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gorm.io/gorm"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm Store", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(":memory:")
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	Describe("GroupStore", func() {
		It("creates a group and generates an ID", func() {
			g := &store.NodeGroup{Name: "production", Description: "prod env"}
			Expect(s.Create(ctx, g)).To(Succeed())
			Expect(g.ID).NotTo(BeEmpty())
		})

		It("lists groups", func() {
			Expect(s.Create(ctx, &store.NodeGroup{Name: "a"})).To(Succeed())
			Expect(s.Create(ctx, &store.NodeGroup{Name: "b"})).To(Succeed())

			groups, err := s.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))
		})

		It("gets by ID", func() {
			g := &store.NodeGroup{Name: "staging"}
			Expect(s.Create(ctx, g)).To(Succeed())

			found, err := s.GetByID(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("staging"))
		})

		It("gets by name", func() {
			g := &store.NodeGroup{Name: "dev"}
			Expect(s.Create(ctx, g)).To(Succeed())

			found, err := s.GetByName(ctx, "dev")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.ID).To(Equal(g.ID))
		})

		It("updates a group", func() {
			g := &store.NodeGroup{Name: "old-name"}
			Expect(s.Create(ctx, g)).To(Succeed())

			g.Name = "new-name"
			g.Description = "updated"
			Expect(s.Update(ctx, g)).To(Succeed())

			found, err := s.GetByID(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Name).To(Equal("new-name"))
			Expect(found.Description).To(Equal("updated"))
		})

		It("deletes a group", func() {
			g := &store.NodeGroup{Name: "to-delete"}
			Expect(s.Create(ctx, g)).To(Succeed())

			Expect(s.Delete(ctx, g.ID)).To(Succeed())

			_, err := s.GetByID(ctx, g.ID)
			Expect(err).To(HaveOccurred())
		})

		It("detaches member nodes when a group is deleted", func() {
			g := &store.NodeGroup{Name: "to-delete-with-nodes"}
			Expect(s.Create(ctx, g)).To(Succeed())

			n1 := &store.ManagedNode{MachineID: "del-n1", GroupID: new(g.ID)}
			n2 := &store.ManagedNode{MachineID: "del-n2", GroupID: new(g.ID)}
			Expect(s.Register(ctx, n1)).To(Succeed())
			Expect(s.Register(ctx, n2)).To(Succeed())

			Expect(s.Delete(ctx, g.ID)).To(Succeed())

			// Group gone.
			_, err := s.GetByID(ctx, g.ID)
			Expect(err).To(HaveOccurred())

			// Nodes still registered, but no longer attached to the deleted group.
			found1, err := s.NodeGetByID(ctx, n1.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found1.GroupID).To(BeNil())

			found2, err := s.NodeGetByID(ctx, n2.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found2.GroupID).To(BeNil())
		})

		It("counts nodes per group, skipping empty and ungrouped", func() {
			a := &store.NodeGroup{Name: "count-a"}
			b := &store.NodeGroup{Name: "count-b"}
			Expect(s.Create(ctx, a)).To(Succeed())
			Expect(s.Create(ctx, b)).To(Succeed())

			Expect(s.Register(ctx, &store.ManagedNode{MachineID: "cnt-1", GroupID: new(a.ID)})).To(Succeed())
			Expect(s.Register(ctx, &store.ManagedNode{MachineID: "cnt-2", GroupID: new(a.ID)})).To(Succeed())
			Expect(s.Register(ctx, &store.ManagedNode{MachineID: "cnt-3"})).To(Succeed())

			counts, err := s.GroupNodeCounts(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(counts).To(Equal(map[string]int{a.ID: 2}))
			Expect(counts).NotTo(HaveKey(b.ID))
		})

		It("rejects duplicate name", func() {
			Expect(s.Create(ctx, &store.NodeGroup{Name: "dup"})).To(Succeed())
			err := s.Create(ctx, &store.NodeGroup{Name: "dup"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("NodeStore", func() {
		It("registers a node with generated ID, API key, and Registered phase", func() {
			n := &store.ManagedNode{MachineID: "m1", Hostname: "host1"}
			Expect(s.Register(ctx, n)).To(Succeed())
			Expect(n.ID).NotTo(BeEmpty())
			Expect(n.APIKey).NotTo(BeEmpty())
			Expect(len(n.APIKey)).To(Equal(64)) // 32 bytes hex
			Expect(n.Phase).To(Equal(store.PhaseRegistered))
		})

		It("rejects duplicate machineID", func() {
			Expect(s.Register(ctx, &store.ManagedNode{MachineID: "m1"})).To(Succeed())
			err := s.Register(ctx, &store.ManagedNode{MachineID: "m1"})
			Expect(err).To(HaveOccurred())
		})

		It("gets by ID", func() {
			n := &store.ManagedNode{MachineID: "m2", Hostname: "h2"}
			Expect(s.Register(ctx, n)).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Hostname).To(Equal("h2"))
		})

		It("gets by MachineID", func() {
			n := &store.ManagedNode{MachineID: "m3"}
			Expect(s.Register(ctx, n)).To(Succeed())

			found, err := s.GetByMachineID(ctx, "m3")
			Expect(err).NotTo(HaveOccurred())
			Expect(found.ID).To(Equal(n.ID))
		})

		It("gets by APIKey", func() {
			n := &store.ManagedNode{MachineID: "m4"}
			Expect(s.Register(ctx, n)).To(Succeed())

			found, err := s.GetByAPIKey(ctx, n.APIKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.ID).To(Equal(n.ID))
		})

		It("lists all nodes", func() {
			Expect(s.Register(ctx, &store.ManagedNode{MachineID: "a"})).To(Succeed())
			Expect(s.Register(ctx, &store.ManagedNode{MachineID: "b"})).To(Succeed())

			nodes, err := s.NodeList(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(2))
		})

		It("lists by group", func() {
			g := &store.NodeGroup{Name: "grp"}
			Expect(s.Create(ctx, g)).To(Succeed())

			n1 := &store.ManagedNode{MachineID: "n1", GroupID: new(g.ID)}
			n2 := &store.ManagedNode{MachineID: "n2"}
			Expect(s.Register(ctx, n1)).To(Succeed())
			Expect(s.Register(ctx, n2)).To(Succeed())

			nodes, err := s.ListByGroup(ctx, g.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].MachineID).To(Equal("n1"))
		})

		It("lists by labels with AND matching", func() {
			n1 := &store.ManagedNode{MachineID: "l1", Labels: map[string]string{"env": "prod", "tier": "web"}}
			n2 := &store.ManagedNode{MachineID: "l2", Labels: map[string]string{"env": "prod", "tier": "db"}}
			n3 := &store.ManagedNode{MachineID: "l3", Labels: map[string]string{"env": "staging"}}
			Expect(s.Register(ctx, n1)).To(Succeed())
			Expect(s.Register(ctx, n2)).To(Succeed())
			Expect(s.Register(ctx, n3)).To(Succeed())

			nodes, err := s.ListByLabels(ctx, map[string]string{"env": "prod", "tier": "web"})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].MachineID).To(Equal("l1"))
		})

		It("lists by selector combining group, labels, and nodeIDs", func() {
			g := &store.NodeGroup{Name: "sel-grp"}
			Expect(s.Create(ctx, g)).To(Succeed())

			n1 := &store.ManagedNode{MachineID: "s1", GroupID: new(g.ID), Labels: map[string]string{"role": "worker"}}
			n2 := &store.ManagedNode{MachineID: "s2", GroupID: new(g.ID), Labels: map[string]string{"role": "worker"}}
			n3 := &store.ManagedNode{MachineID: "s3", GroupID: new(g.ID), Labels: map[string]string{"role": "control"}}
			Expect(s.Register(ctx, n1)).To(Succeed())
			Expect(s.Register(ctx, n2)).To(Succeed())
			Expect(s.Register(ctx, n3)).To(Succeed())

			// group + labels + nodeIDs all AND'd
			nodes, err := s.ListBySelector(ctx, store.CommandSelector{
				GroupID: g.ID,
				Labels:  map[string]string{"role": "worker"},
				NodeIDs: []string{n1.ID},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
			Expect(nodes[0].ID).To(Equal(n1.ID))
		})

		It("lists by selector with only group", func() {
			g := &store.NodeGroup{Name: "only-grp"}
			Expect(s.Create(ctx, g)).To(Succeed())

			n1 := &store.ManagedNode{MachineID: "og1", GroupID: new(g.ID)}
			n2 := &store.ManagedNode{MachineID: "og2"}
			Expect(s.Register(ctx, n1)).To(Succeed())
			Expect(s.Register(ctx, n2)).To(Succeed())

			nodes, err := s.ListBySelector(ctx, store.CommandSelector{GroupID: g.ID})
			Expect(err).NotTo(HaveOccurred())
			Expect(nodes).To(HaveLen(1))
		})

		It("updates heartbeat", func() {
			n := &store.ManagedNode{MachineID: "hb1"}
			Expect(s.Register(ctx, n)).To(Succeed())

			osRel := map[string]string{"name": "Kairos", "version": "1.0"}
			addrs := []store.NodeAddress{
				{Type: "InternalIP", Address: "10.0.10.21"},
				{Type: "Hostname", Address: "hb1"},
			}
			Expect(s.UpdateHeartbeat(ctx, n.ID, "v0.5.0", osRel, addrs, "active", "hb1-renamed", "203.0.113.5")).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.PhaseOnline))
			Expect(found.AgentVersion).To(Equal("v0.5.0"))
			Expect(found.OSRelease).To(HaveKeyWithValue("name", "Kairos"))
			Expect(found.LastHeartbeat).NotTo(BeNil())
			Expect(found.Addresses).To(Equal(addrs))
			Expect(found.BootState).To(Equal("active"))
			Expect(found.Hostname).To(Equal("hb1-renamed"))
			Expect(found.RemoteIP).To(Equal("203.0.113.5"))
		})

		It("preserves addresses, boot state, hostname and remote IP when a heartbeat omits them", func() {
			n := &store.ManagedNode{
				MachineID: "hb2",
				Hostname:  "hb2-host",
				Addresses: []store.NodeAddress{{Type: "InternalIP", Address: "10.0.10.22"}},
				BootState: "active",
				RemoteIP:  "198.51.100.9",
			}
			Expect(s.Register(ctx, n)).To(Succeed())

			// An older agent (or the WS heartbeat path) sends nil/"" — must not wipe.
			Expect(s.UpdateHeartbeat(ctx, n.ID, "v0.6.0", nil, nil, "", "", "")).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Addresses).To(Equal([]store.NodeAddress{{Type: "InternalIP", Address: "10.0.10.22"}}))
			Expect(found.BootState).To(Equal("active"))
			Expect(found.Hostname).To(Equal("hb2-host"))
			Expect(found.RemoteIP).To(Equal("198.51.100.9"))
		})

		// The bug this closes: a node registers while cloud-init has not yet
		// applied `hostname: kairos-{{ trunc 4 .MachineID }}`, so it lands as the
		// image default and used to stay that way for the node's whole life. The
		// heartbeat has to be able to move it (kairos-io/kairos#4196).
		It("moves the hostname a node registered with to the one it now reports", func() {
			n := &store.ManagedNode{MachineID: "a1b2c3d4", Hostname: "kairos"}
			Expect(s.Register(ctx, n)).To(Succeed())

			Expect(s.UpdateHeartbeat(ctx, n.ID, "v0.6.0", nil, nil, "", "kairos-a1b2", "")).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Hostname).To(Equal("kairos-a1b2"))

			// Idempotent: the next heartbeat reports the same name and nothing
			// churns, and a rename later in the node's life still lands.
			Expect(s.UpdateHeartbeat(ctx, n.ID, "v0.6.0", nil, nil, "", "kairos-a1b2", "")).To(Succeed())
			found, err = s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Hostname).To(Equal("kairos-a1b2"))

			Expect(s.UpdateHeartbeat(ctx, n.ID, "v0.6.0", nil, nil, "", "renamed-later", "")).To(Succeed())
			found, err = s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Hostname).To(Equal("renamed-later"))
		})

		It("updates phase", func() {
			n := &store.ManagedNode{MachineID: "ph1"}
			Expect(s.Register(ctx, n)).To(Succeed())

			Expect(s.UpdatePhase(ctx, n.ID, store.PhaseOffline)).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.PhaseOffline))
		})

		It("sets group", func() {
			g := &store.NodeGroup{Name: "target-grp"}
			Expect(s.Create(ctx, g)).To(Succeed())

			n := &store.ManagedNode{MachineID: "sg1"}
			Expect(s.Register(ctx, n)).To(Succeed())

			Expect(s.SetGroup(ctx, n.ID, &g.ID)).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.GroupID).To(HaveValue(Equal(g.ID)))
			Expect(found.Group).NotTo(BeNil())
			Expect(found.Group.Name).To(Equal("target-grp"))
		})

		It("refuses to set a group on an unknown node", func() {
			g := &store.NodeGroup{Name: "orphan-grp"}
			Expect(s.Create(ctx, g)).To(Succeed())

			Expect(s.SetGroup(ctx, "no-such-node", &g.ID)).To(MatchError(gorm.ErrRecordNotFound))
		})

		It("refuses to move a node into an unknown group", func() {
			n := &store.ManagedNode{MachineID: "sg2"}
			Expect(s.Register(ctx, n)).To(Succeed())

			missing := "no-such-group"
			Expect(s.SetGroup(ctx, n.ID, &missing)).To(MatchError(gorm.ErrRecordNotFound))

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.GroupID).To(BeNil())
		})

		It("moves a node out of its group", func() {
			g := &store.NodeGroup{Name: "leave-grp"}
			Expect(s.Create(ctx, g)).To(Succeed())
			n := &store.ManagedNode{MachineID: "sg3", GroupID: &g.ID}
			Expect(s.Register(ctx, n)).To(Succeed())

			Expect(s.SetGroup(ctx, n.ID, nil)).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.GroupID).To(BeNil())
		})

		It("refuses to set labels on an unknown node", func() {
			Expect(s.SetLabels(ctx, "no-such-node", map[string]string{"a": "b"})).To(MatchError(gorm.ErrRecordNotFound))
		})

		It("sets labels", func() {
			n := &store.ManagedNode{MachineID: "sl1"}
			Expect(s.Register(ctx, n)).To(Succeed())

			labels := map[string]string{"env": "test", "team": "infra"}
			Expect(s.SetLabels(ctx, n.ID, labels)).To(Succeed())

			found, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Labels).To(Equal(labels))
		})

		It("deletes a node and its commands", func() {
			n := &store.ManagedNode{MachineID: "del1"}
			Expect(s.Register(ctx, n)).To(Succeed())

			cmd := &store.NodeCommand{ManagedNodeID: n.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.NodeDelete(ctx, n.ID)).To(Succeed())

			_, err := s.NodeGetByID(ctx, n.ID)
			Expect(err).To(HaveOccurred())

			cmds, err := s.ListByNode(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(BeEmpty())
		})

		It("deletes a node's extension rows with the node", func() {
			n := &store.ManagedNode{MachineID: "del-ext"}
			Expect(s.Register(ctx, n)).To(Succeed())
			other := &store.ManagedNode{MachineID: "keep-ext"}
			Expect(s.Register(ctx, other)).To(Succeed())

			for _, nodeID := range []string{n.ID, other.ID} {
				Expect(s.NodeExtensionUpsert(ctx, &store.NodeExtensionRow{
					NodeID: nodeID, Name: "foo", Type: "sysext", BootState: "active", Version: "1",
				})).To(Succeed())
			}

			Expect(s.NodeDelete(ctx, n.ID)).To(Succeed())

			rows, err := s.NodeExtensionListForNode(ctx, n.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())

			rows, err = s.NodeExtensionListForExtensionByName(ctx, "sysext", "foo")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(HaveLen(1))
			Expect(rows[0].NodeID).To(Equal(other.ID))
		})
	})

	Describe("CommandStore", func() {
		var node *store.ManagedNode

		BeforeEach(func() {
			node = &store.ManagedNode{MachineID: "cmd-node"}
			Expect(s.Register(ctx, node)).To(Succeed())
		})

		It("creates a command with Pending phase and generated ID", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdUpgrade}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())
			Expect(cmd.ID).NotTo(BeEmpty())
			Expect(cmd.Phase).To(Equal(store.CommandPending))
		})

		It("gets pending commands (not expired)", func() {
			future := time.Now().Add(1 * time.Hour)
			past := time.Now().Add(-1 * time.Hour)

			cmd1 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec, ExpiresAt: &future}
			cmd2 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec, ExpiresAt: &past}
			cmd3 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec} // no expiry
			Expect(s.CommandCreate(ctx, cmd1)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd2)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd3)).To(Succeed())

			// Mark cmd2 as delivered so it's not pending, but it's expired anyway
			// Actually cmd2 is still Pending but expired -- it should be excluded
			pending, err := s.GetPending(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(HaveLen(2)) // cmd1 (future expiry) and cmd3 (no expiry)
		})

		It("marks commands as delivered", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdReset}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.MarkDelivered(ctx, []string{cmd.ID})).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandDelivered))
			Expect(found.DeliveredAt).NotTo(BeNil())
		})

		It("claims a pending command for delivery exactly once", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdReset}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			// First claim wins and flips the phase to Delivered.
			claimed, err := s.ClaimForDelivery(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).To(BeTrue())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandDelivered))
			Expect(found.DeliveredAt).NotTo(BeNil())

			// Second claim sees a non-Pending row and loses.
			claimed, err = s.ClaimForDelivery(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).To(BeFalse())
		})

		It("does not claim a missing command", func() {
			claimed, err := s.ClaimForDelivery(ctx, "no-such-command")
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).To(BeFalse())
		})

		It("does not claim an expired pending command", func() {
			expired := time.Now().Add(-time.Minute)
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdReset, ExpiresAt: &expired}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			claimed, err := s.ClaimForDelivery(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(claimed).To(BeFalse())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandPending))
			Expect(found.DeliveredAt).To(BeNil())
		})

		It("yields exactly one winner under concurrent claims", func() {
			// Use a file-backed DB so concurrent goroutines share one SQLite
			// database with WAL/busy_timeout, rather than the per-connection
			// :memory: DB which has no table on a fresh pooled connection.
			fs, err := gormstore.New(filepath.Join(GinkgoT().TempDir(), "claim.db"))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = fs.Close() })

			fnode := &store.ManagedNode{MachineID: "claim-node"}
			Expect(fs.Register(ctx, fnode)).To(Succeed())
			cmd := &store.NodeCommand{ManagedNodeID: fnode.ID, Command: store.CmdReset}
			Expect(fs.CommandCreate(ctx, cmd)).To(Succeed())

			const n = 16
			type claimResult struct {
				ok  bool
				err error
			}
			results := make(chan claimResult, n)
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					// Assertions must run on the spec goroutine; collect results
					// and check them after the wait.
					ok, claimErr := fs.ClaimForDelivery(ctx, cmd.ID)
					results <- claimResult{ok: ok, err: claimErr}
				}()
			}
			wg.Wait()
			close(results)

			wins := 0
			for r := range results {
				Expect(r.err).NotTo(HaveOccurred())
				if r.ok {
					wins++
				}
			}
			Expect(wins).To(Equal(1))
		})

		It("updates status to completed with result and completedAt", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.UpdateStatus(ctx, cmd.ID, store.CommandCompleted, "ok")).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandCompleted))
			Expect(found.Result).To(Equal("ok"))
			Expect(found.CompletedAt).NotTo(BeNil())
		})

		It("updates status to failed with result and completedAt", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.UpdateStatus(ctx, cmd.ID, store.CommandFailed, "error occurred")).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandFailed))
			Expect(found.Result).To(Equal("error occurred"))
			Expect(found.CompletedAt).NotTo(BeNil())
		})

		It("updates status to running without completedAt", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.UpdateStatus(ctx, cmd.ID, store.CommandRunning, "")).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandRunning))
			Expect(found.CompletedAt).To(BeNil())
		})

		It("reports ErrCommandNotFound when updating a command that does not exist", func() {
			Expect(s.UpdateStatus(ctx, "no-such-command", store.CommandCompleted, "ok")).
				To(MatchError(store.ErrCommandNotFound))
		})

		It("updates status scoped to the owning node", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.UpdateStatusForNode(ctx, cmd.ID, node.ID, store.CommandCompleted, "ok")).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandCompleted))
			Expect(found.Result).To(Equal("ok"))
		})

		It("refuses a node-scoped update for another node and reports ErrCommandNotFound", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			// Zero rows match (wrong node) — must surface as ErrCommandNotFound,
			// not a silent success, despite gorm Updates returning a nil error.
			err := s.UpdateStatusForNode(ctx, cmd.ID, "other-node", store.CommandCompleted, "pwned")
			Expect(err).To(MatchError(store.ErrCommandNotFound))

			found, gerr := s.CommandGetByID(ctx, cmd.ID)
			Expect(gerr).NotTo(HaveOccurred())
			Expect(found.Phase).To(Equal(store.CommandPending))
			Expect(found.Result).To(BeEmpty())
		})

		It("refuses a node-scoped update for a non-existent command", func() {
			err := s.UpdateStatusForNode(ctx, "no-such-id", node.ID, store.CommandCompleted, "x")
			Expect(err).To(MatchError(store.ErrCommandNotFound))
		})

		It("lists all commands by node", func() {
			Expect(s.CommandCreate(ctx, &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec})).To(Succeed())
			Expect(s.CommandCreate(ctx, &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdReset})).To(Succeed())

			cmds, err := s.ListByNode(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(HaveLen(2))
		})

		It("gets command by ID", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdUpgrade, Args: map[string]string{"version": "1.2"}}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.Command).To(Equal(store.CmdUpgrade))
			Expect(found.Args).To(HaveKeyWithValue("version", "1.2"))
		})
	})

	Describe("CommandStore Delete", func() {
		var node *store.ManagedNode

		BeforeEach(func() {
			node = &store.ManagedNode{MachineID: "cmd-del-node"}
			Expect(s.Register(ctx, node)).To(Succeed())
		})

		It("should delete a single command by ID", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())

			Expect(s.CommandDelete(ctx, cmd.ID)).To(Succeed())

			_, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("CommandStore DeleteTerminal", func() {
		var node *store.ManagedNode

		BeforeEach(func() {
			node = &store.ManagedNode{MachineID: "cmd-delterm-node"}
			Expect(s.Register(ctx, node)).To(Succeed())
		})

		It("should delete all Completed and Failed commands for a node", func() {
			cmd1 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			cmd2 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			cmd3 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			cmd4 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd1)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd2)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd3)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd4)).To(Succeed())

			Expect(s.UpdateStatus(ctx, cmd1.ID, store.CommandCompleted, "done")).To(Succeed())
			Expect(s.UpdateStatus(ctx, cmd2.ID, store.CommandFailed, "error")).To(Succeed())
			Expect(s.UpdateStatus(ctx, cmd3.ID, store.CommandRunning, "")).To(Succeed())
			// cmd4 stays Pending

			Expect(s.CommandDeleteTerminal(ctx, node.ID)).To(Succeed())

			cmds, err := s.ListByNode(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(HaveLen(2)) // Running + Pending remain
		})

		It("should NOT delete Pending or Running commands", func() {
			cmd1 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			cmd2 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd1)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd2)).To(Succeed())

			Expect(s.UpdateStatus(ctx, cmd2.ID, store.CommandRunning, "")).To(Succeed())

			Expect(s.CommandDeleteTerminal(ctx, node.ID)).To(Succeed())

			cmds, err := s.ListByNode(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(HaveLen(2))
		})

		It("should only affect the specified node's commands", func() {
			node2 := &store.ManagedNode{MachineID: "cmd-delterm-node2"}
			Expect(s.Register(ctx, node2)).To(Succeed())

			cmd1 := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			cmd2 := &store.NodeCommand{ManagedNodeID: node2.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd1)).To(Succeed())
			Expect(s.CommandCreate(ctx, cmd2)).To(Succeed())

			Expect(s.UpdateStatus(ctx, cmd1.ID, store.CommandCompleted, "done")).To(Succeed())
			Expect(s.UpdateStatus(ctx, cmd2.ID, store.CommandCompleted, "done")).To(Succeed())

			Expect(s.CommandDeleteTerminal(ctx, node.ID)).To(Succeed())

			// node's commands should be gone
			cmds1, err := s.ListByNode(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds1).To(BeEmpty())

			// node2's commands should remain
			cmds2, err := s.ListByNode(ctx, node2.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds2).To(HaveLen(1))
		})

		It("should delete Canceled commands too", func() {
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())
			Expect(s.UpdateStatus(ctx, cmd.ID, store.CommandCanceled, "")).To(Succeed())

			Expect(s.CommandDeleteTerminal(ctx, node.ID)).To(Succeed())

			cmds, err := s.ListByNode(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(BeEmpty())
		})

		It("should delete Expired commands too", func() {
			past := time.Now().Add(-time.Hour)
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec, ExpiresAt: &past}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())
			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())

			Expect(s.CommandDeleteTerminal(ctx, node.ID)).To(Succeed())

			cmds, err := s.ListByNode(ctx, node.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cmds).To(BeEmpty())
		})
	})

	Describe("CreateBatch", func() {
		register := func(machineID string) *store.ManagedNode {
			n := &store.ManagedNode{MachineID: machineID}
			Expect(s.Register(ctx, n)).To(Succeed())
			return n
		}

		node := func(id string) *store.ManagedNode {
			got, err := s.NodeGetByID(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			return got
		}

		batchOf := func(command string, ids []string, nodes ...*store.ManagedNode) []*store.NodeCommand {
			cmds := make([]*store.NodeCommand, 0, len(nodes))
			for i, n := range nodes {
				cmds = append(cmds, &store.NodeCommand{
					ID:            ids[i],
					ManagedNodeID: n.ID,
					Command:       command,
					Phase:         store.CommandPending,
				})
			}
			return cmds
		}

		It("writes every row with the ids the caller gave it", func() {
			n1, n2 := register("m1"), register("m2")

			Expect(s.CreateBatch(ctx, batchOf(store.CmdUpgrade, []string{"c1", "c2"}, n1, n2))).To(Succeed())

			for _, id := range []string{"c1", "c2"} {
				got, err := s.CommandGetByID(ctx, id)
				Expect(err).NotTo(HaveOccurred())
				Expect(got.Phase).To(Equal(store.CommandPending))
			}
			Expect(s.ListByNode(ctx, n1.ID)).To(ConsistOf(HaveField("ID", "c1")))
			Expect(s.ListByNode(ctx, n2.ID)).To(ConsistOf(HaveField("ID", "c2")))
		})

		It("marks every upgrade target pending in the same write", func() {
			n1, n2, other := register("m1"), register("m2"), register("m3")

			Expect(s.CreateBatch(ctx, batchOf(store.CmdUpgrade, []string{"c1", "c2"}, n1, n2))).To(Succeed())

			for _, id := range []string{n1.ID, n2.ID} {
				got := node(id)
				Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
				Expect(got.UpgradeRequestedAt).NotTo(BeNil())
				Expect(got.ResetState).To(BeEmpty())
			}
			Expect(node(other.ID).UpgradeState).To(BeEmpty())
		})

		It("marks every reset target pending in the same write", func() {
			n1, n2 := register("m1"), register("m2")

			Expect(s.CreateBatch(ctx, batchOf(store.CmdReset, []string{"c1", "c2"}, n1, n2))).To(Succeed())

			for _, id := range []string{n1.ID, n2.ID} {
				got := node(id)
				Expect(got.ResetState).To(Equal(store.ResetStatePending))
				Expect(got.ResetRequestedAt).NotTo(BeNil())
				Expect(got.UpgradeState).To(BeEmpty())
			}
		})

		It("leaves the lifecycle alone for a command that does not reboot the node", func() {
			n1 := register("m1")

			Expect(s.CreateBatch(ctx, batchOf("reboot", []string{"c1"}, n1))).To(Succeed())

			got := node(n1.ID)
			Expect(got.UpgradeState).To(BeEmpty())
			Expect(got.ResetState).To(BeEmpty())
		})

		// A node whose earlier upgrade or reset is still waiting for its
		// reboot keeps the request time of that operation, so the resolver
		// that runs when it re-registers judges the operation that is in
		// flight.
		It("leaves an upgrade that is already pending and its request time alone", func() {
			n1, n2 := register("m1"), register("m2")
			issueCommand(ctx, s, n1.ID, store.CmdUpgrade)
			requested := *node(n1.ID).UpgradeRequestedAt

			Expect(s.CreateBatch(ctx, batchOf(store.CmdUpgrade, []string{"c1", "c2"}, n1, n2))).To(Succeed())

			got := node(n1.ID)
			Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
			Expect(*got.UpgradeRequestedAt).To(BeTemporally("==", requested))
			Expect(node(n2.ID).UpgradeState).To(Equal(store.UpgradeStatePending))
		})

		It("marks an upgrade target whose last upgrade finished", func() {
			n1 := register("m1")
			issueCommand(ctx, s, n1.ID, store.CmdUpgrade)
			_, err := s.AdvanceUpgrade(ctx, n1.ID, []string{store.UpgradeStatePending}, store.UpgradeStateDone, true)
			Expect(err).NotTo(HaveOccurred())
			requested := *node(n1.ID).UpgradeRequestedAt

			Expect(s.CreateBatch(ctx, batchOf(store.CmdUpgrade, []string{"c1"}, n1))).To(Succeed())

			got := node(n1.ID)
			Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
			Expect(*got.UpgradeRequestedAt).To(BeTemporally(">", requested))
		})

		DescribeTable("leaves a reset that is already in flight and its request time alone",
			func(state string) {
				n1, n2 := register("m1"), register("m2")
				issueCommand(ctx, s, n1.ID, store.CmdReset)
				if state != store.ResetStatePending {
					_, err := s.AdvanceReset(ctx, n1.ID, []string{store.ResetStatePending}, state, false)
					Expect(err).NotTo(HaveOccurred())
				}
				requested := *node(n1.ID).ResetRequestedAt

				Expect(s.CreateBatch(ctx, batchOf(store.CmdReset, []string{"c1", "c2"}, n1, n2))).To(Succeed())

				got := node(n1.ID)
				Expect(got.ResetState).To(Equal(state))
				Expect(*got.ResetRequestedAt).To(BeTemporally("==", requested))
				Expect(node(n2.ID).ResetState).To(Equal(store.ResetStatePending))
			},
			Entry("pending", store.ResetStatePending),
			Entry("in-progress", store.ResetStateInProgress),
		)

		// An agent may claim any committed Pending row, so a batch that is
		// half written can be half delivered. A failure part way through has
		// to leave nothing behind.
		It("writes nothing when one row of the batch cannot be stored", func() {
			n1, n2 := register("m1"), register("m2")

			err := s.CreateBatch(ctx, batchOf(store.CmdUpgrade, []string{"dup", "dup"}, n1, n2))
			Expect(err).To(HaveOccurred())

			for _, id := range []string{n1.ID, n2.ID} {
				Expect(s.ListByNode(ctx, id)).To(BeEmpty())
				Expect(node(id).UpgradeState).To(BeEmpty())
				Expect(node(id).UpgradeRequestedAt).To(BeNil())
			}
		})
	})

	Describe("CommandExpireBefore", func() {
		var node *store.ManagedNode
		var past, future time.Time

		BeforeEach(func() {
			node = &store.ManagedNode{MachineID: "cmd-expire-node"}
			Expect(s.Register(ctx, node)).To(Succeed())
			past = time.Now().Add(-time.Hour)
			future = time.Now().Add(time.Hour)
		})

		// create makes a command in the given phase; CommandCreate always writes
		// Pending, so a non-Pending phase needs the follow-up UpdateStatus.
		create := func(phase string, expires *time.Time) *store.NodeCommand {
			GinkgoHelper()
			cmd := &store.NodeCommand{ManagedNodeID: node.ID, Command: store.CmdExec, ExpiresAt: expires}
			Expect(s.CommandCreate(ctx, cmd)).To(Succeed())
			if phase != store.CommandPending {
				Expect(s.UpdateStatus(ctx, cmd.ID, phase, "")).To(Succeed())
			}
			return cmd
		}

		phaseOf := func(id string) string {
			GinkgoHelper()
			found, err := s.CommandGetByID(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			return found.Phase
		}

		It("expires overdue Pending, Delivered and Running commands", func() {
			pending := create(store.CommandPending, &past)
			delivered := create(store.CommandDelivered, &past)
			running := create(store.CommandRunning, &past)

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())

			Expect(phaseOf(pending.ID)).To(Equal(store.CommandExpired))
			Expect(phaseOf(delivered.ID)).To(Equal(store.CommandExpired))
			Expect(phaseOf(running.ID)).To(Equal(store.CommandExpired))
		})

		It("stamps CompletedAt on the command it expires", func() {
			cmd := create(store.CommandPending, &past)

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())

			found, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.CompletedAt).NotTo(BeNil())
		})

		It("leaves a command whose deadline has not passed, and one with no deadline", func() {
			notYet := create(store.CommandPending, &future)
			noDeadline := create(store.CommandPending, nil)

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())

			Expect(phaseOf(notYet.ID)).To(Equal(store.CommandPending))
			Expect(phaseOf(noDeadline.ID)).To(Equal(store.CommandPending))
		})

		It("does not rewrite a terminal phase, so a late report keeps its outcome", func() {
			completed := create(store.CommandCompleted, &past)
			failed := create(store.CommandFailed, &past)

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())

			Expect(phaseOf(completed.ID)).To(Equal(store.CommandCompleted))
			Expect(phaseOf(failed.ID)).To(Equal(store.CommandFailed))
		})

		It("is idempotent", func() {
			cmd := create(store.CommandPending, &past)

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())
			first, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())
			second, err := s.CommandGetByID(ctx, cmd.ID)
			Expect(err).NotTo(HaveOccurred())

			Expect(second.Phase).To(Equal(store.CommandExpired))
			Expect(second.CompletedAt.Equal(*first.CompletedAt)).To(BeTrue())
		})

		It("only affects the named node", func() {
			node2 := &store.ManagedNode{MachineID: "cmd-expire-node2"}
			Expect(s.Register(ctx, node2)).To(Succeed())
			other := &store.NodeCommand{ManagedNodeID: node2.ID, Command: store.CmdExec, ExpiresAt: &past}
			Expect(s.CommandCreate(ctx, other)).To(Succeed())

			mine := create(store.CommandPending, &past)

			Expect(s.CommandExpireBefore(ctx, node.ID, time.Now())).To(Succeed())

			Expect(phaseOf(mine.ID)).To(Equal(store.CommandExpired))
			Expect(phaseOf(other.ID)).To(Equal(store.CommandPending))
		})
	})

	Describe("ArtifactStore DeleteByPhase", func() {
		It("should delete all Error-phase artifacts", func() {
			rec1 := &store.ArtifactRecord{ID: "art-1", Phase: store.ArtifactError, BaseImage: "img1"}
			rec2 := &store.ArtifactRecord{ID: "art-2", Phase: store.ArtifactReady, BaseImage: "img2"}
			rec3 := &store.ArtifactRecord{ID: "art-3", Phase: store.ArtifactError, BaseImage: "img3"}
			Expect(s.ArtifactCreate(ctx, rec1)).To(Succeed())
			Expect(s.ArtifactCreate(ctx, rec2)).To(Succeed())
			Expect(s.ArtifactCreate(ctx, rec3)).To(Succeed())

			Expect(s.ArtifactDeleteByPhase(ctx, store.ArtifactError)).To(Succeed())

			records, err := s.ArtifactList(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(records).To(HaveLen(1))
			Expect(records[0].ID).To(Equal("art-2"))
		})

		It("should NOT delete Ready or Building artifacts", func() {
			rec1 := &store.ArtifactRecord{ID: "art-4", Phase: store.ArtifactReady, BaseImage: "img4"}
			rec2 := &store.ArtifactRecord{ID: "art-5", Phase: store.ArtifactBuilding, BaseImage: "img5"}
			Expect(s.ArtifactCreate(ctx, rec1)).To(Succeed())
			Expect(s.ArtifactCreate(ctx, rec2)).To(Succeed())

			Expect(s.ArtifactDeleteByPhase(ctx, store.ArtifactError)).To(Succeed())

			records, err := s.ArtifactList(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(records).To(HaveLen(2))
		})

		It("removes the bundle rows of the deleted artifacts only", func() {
			Expect(s.ArtifactCreate(ctx, &store.ArtifactRecord{ID: "art-err", Phase: store.ArtifactError, BaseImage: "img"})).To(Succeed())
			Expect(s.ArtifactCreate(ctx, &store.ArtifactRecord{ID: "art-ok", Phase: store.ArtifactReady, BaseImage: "img"})).To(Succeed())
			Expect(s.BundleReplaceForArtifact(ctx, "art-err", []store.ArtifactExtensionBundle{{ExtensionName: "ext-a", ExtensionType: "sysext"}})).To(Succeed())
			Expect(s.BundleReplaceForArtifact(ctx, "art-ok", []store.ArtifactExtensionBundle{{ExtensionName: "ext-b", ExtensionType: "sysext"}})).To(Succeed())

			Expect(s.ArtifactDeleteByPhase(ctx, store.ArtifactError)).To(Succeed())

			ids, err := s.BundleArtifactsReferencingExtension(ctx, "ext-a")
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(BeEmpty())
			ids, err = s.BundleArtifactsReferencingExtension(ctx, "ext-b")
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf("art-ok"))
		})
	})

	Describe("ArtifactStore Delete", func() {
		It("removes the artifact's bundle rows and leaves other artifacts' rows", func() {
			Expect(s.ArtifactCreate(ctx, &store.ArtifactRecord{ID: "art-del", Phase: store.ArtifactReady, BaseImage: "img"})).To(Succeed())
			Expect(s.ArtifactCreate(ctx, &store.ArtifactRecord{ID: "art-keep", Phase: store.ArtifactReady, BaseImage: "img"})).To(Succeed())
			Expect(s.BundleReplaceForArtifact(ctx, "art-del", []store.ArtifactExtensionBundle{{ExtensionName: "ext-a", ExtensionType: "sysext"}})).To(Succeed())
			Expect(s.BundleReplaceForArtifact(ctx, "art-keep", []store.ArtifactExtensionBundle{{ExtensionName: "ext-a", ExtensionType: "sysext"}})).To(Succeed())

			Expect(s.ArtifactDelete(ctx, "art-del")).To(Succeed())

			rows, err := s.BundleListForArtifact(ctx, "art-del")
			Expect(err).NotTo(HaveOccurred())
			Expect(rows).To(BeEmpty())
			ids, err := s.BundleArtifactsReferencingExtension(ctx, "ext-a")
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf("art-keep"))
		})
	})

	Describe("ArtifactUpdate vs AppendLog", func() {
		It("does not clobber log lines appended between GetByID and Update", func() {
			rec := &store.ArtifactRecord{ID: "art-race", Phase: store.ArtifactBuilding, BaseImage: "img"}
			Expect(s.ArtifactCreate(ctx, rec)).To(Succeed())

			Expect(s.ArtifactAppendLog(ctx, "art-race", "line-1\n")).To(Succeed())

			snapshot, err := s.ArtifactGetByID(ctx, "art-race")
			Expect(err).NotTo(HaveOccurred())

			Expect(s.ArtifactAppendLog(ctx, "art-race", "line-2\n")).To(Succeed())

			snapshot.Phase = store.ArtifactReady
			snapshot.Message = "done"
			Expect(s.ArtifactUpdate(ctx, snapshot)).To(Succeed())

			logs, err := s.ArtifactGetLogs(ctx, "art-race")
			Expect(err).NotTo(HaveOccurred())
			Expect(logs).To(Equal("line-1\nline-2\n"))

			refreshed, err := s.ArtifactGetByID(ctx, "art-race")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Phase).To(Equal(store.ArtifactReady))
			Expect(refreshed.Message).To(Equal("done"))
		})
	})

	Describe("ArtifactUpdatePhaseMessage", func() {
		It("updates only phase and message and never touches logs", func() {
			rec := &store.ArtifactRecord{ID: "art-phase", Phase: store.ArtifactBuilding, BaseImage: "img"}
			Expect(s.ArtifactCreate(ctx, rec)).To(Succeed())
			Expect(s.ArtifactAppendLog(ctx, "art-phase", "hello\n")).To(Succeed())

			Expect(s.ArtifactUpdatePhaseMessage(ctx, "art-phase", store.ArtifactReady, "ok")).To(Succeed())

			refreshed, err := s.ArtifactGetByID(ctx, "art-phase")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Phase).To(Equal(store.ArtifactReady))
			Expect(refreshed.Message).To(Equal("ok"))
			Expect(refreshed.Logs).To(Equal("hello\n"))
			Expect(refreshed.BaseImage).To(Equal("img"))
		})
	})

	Describe("ArtifactUpdateFiles", func() {
		It("writes only artifact_files and does not clobber a concurrent phase write", func() {
			rec := &store.ArtifactRecord{
				ID: "art-files", Phase: store.ArtifactBuilding, BaseImage: "img",
				UploadToken: "tok",
			}
			Expect(s.ArtifactCreate(ctx, rec)).To(Succeed())

			// A handler snapshot mid-build.
			snapshot, err := s.ArtifactGetByID(ctx, "art-files")
			Expect(err).NotTo(HaveOccurred())

			// watchCRPhase publishes a terminal transition while the handler
			// is still processing the upload.
			Expect(s.ArtifactUpdatePhaseMessage(ctx, "art-files", store.ArtifactReady, "done")).To(Succeed())

			// Handler now writes back the new file list from its stale
			// snapshot. UpdateFiles must NOT overwrite the phase the watcher
			// just committed.
			snapshot.ArtifactFiles = []string{"kairos.iso", "kairos.iso.sha256"}
			Expect(s.ArtifactUpdateFiles(ctx, "art-files", snapshot.ArtifactFiles)).To(Succeed())

			refreshed, err := s.ArtifactGetByID(ctx, "art-files")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Phase).To(Equal(store.ArtifactReady))
			Expect(refreshed.Message).To(Equal("done"))
			Expect(refreshed.ArtifactFiles).To(Equal([]string{"kairos.iso", "kairos.iso.sha256"}))
			Expect(refreshed.UploadToken).To(Equal("tok"))
			Expect(refreshed.BaseImage).To(Equal("img"))
		})
	})

	Describe("ArtifactUpdateNameSaved", func() {
		It("writes only name and saved and does not clobber a concurrent phase write", func() {
			rec := &store.ArtifactRecord{
				ID: "art-name", Name: "old", Phase: store.ArtifactBuilding, BaseImage: "img",
				UploadToken: "tok",
			}
			Expect(s.ArtifactCreate(ctx, rec)).To(Succeed())

			Expect(s.ArtifactUpdatePhaseMessage(ctx, "art-name", store.ArtifactReady, "done")).To(Succeed())
			Expect(s.ArtifactUpdateFiles(ctx, "art-name", []string{"kairos.iso"})).To(Succeed())

			name, saved := "renamed", true
			Expect(s.ArtifactUpdateNameSaved(ctx, "art-name", &name, &saved)).To(Succeed())

			refreshed, err := s.ArtifactGetByID(ctx, "art-name")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Name).To(Equal("renamed"))
			Expect(refreshed.Saved).To(BeTrue())
			Expect(refreshed.Phase).To(Equal(store.ArtifactReady))
			Expect(refreshed.Message).To(Equal("done"))
			Expect(refreshed.ArtifactFiles).To(Equal([]string{"kairos.iso"}))
			Expect(refreshed.UploadToken).To(Equal("tok"))
			Expect(refreshed.BaseImage).To(Equal("img"))
		})

		It("writes zero values and skips nil fields", func() {
			rec := &store.ArtifactRecord{ID: "art-zero", Name: "keep", Saved: true}
			Expect(s.ArtifactCreate(ctx, rec)).To(Succeed())

			unsaved := false
			Expect(s.ArtifactUpdateNameSaved(ctx, "art-zero", nil, &unsaved)).To(Succeed())
			refreshed, err := s.ArtifactGetByID(ctx, "art-zero")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Name).To(Equal("keep"))
			Expect(refreshed.Saved).To(BeFalse())

			empty := ""
			Expect(s.ArtifactUpdateNameSaved(ctx, "art-zero", &empty, nil)).To(Succeed())
			refreshed, err = s.ArtifactGetByID(ctx, "art-zero")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.Name).To(BeEmpty())
			Expect(refreshed.Saved).To(BeFalse())

			Expect(s.ArtifactUpdateNameSaved(ctx, "art-zero", nil, nil)).To(Succeed())
		})
	})

	Describe("ArtifactClearUploadToken", func() {
		It("zeroes upload_token and leaves everything else alone", func() {
			rec := &store.ArtifactRecord{
				ID: "art-clear", Phase: store.ArtifactBuilding, BaseImage: "img",
				UploadToken: "tok", Message: "still going",
			}
			Expect(s.ArtifactCreate(ctx, rec)).To(Succeed())
			Expect(s.ArtifactAppendLog(ctx, "art-clear", "line\n")).To(Succeed())

			Expect(s.ArtifactClearUploadToken(ctx, "art-clear")).To(Succeed())

			refreshed, err := s.ArtifactGetByID(ctx, "art-clear")
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.UploadToken).To(Equal(""))
			Expect(refreshed.Phase).To(Equal(store.ArtifactBuilding))
			Expect(refreshed.Message).To(Equal("still going"))
			Expect(refreshed.Logs).To(Equal("line\n"))
			Expect(refreshed.BaseImage).To(Equal("img"))
		})
	})
})
