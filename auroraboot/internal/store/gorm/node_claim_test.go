package gorm_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm Store node claim", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	// register adds a node to groupID and returns it (with its generated ID).
	register := func(groupID, machineID string) *store.ManagedNode {
		n := &store.ManagedNode{MachineID: machineID, GroupID: new(groupID)}
		Expect(s.Register(ctx, n)).To(Succeed())
		return n
	}

	BeforeEach(func() {
		var err error
		// File-backed so concurrent goroutines share one SQLite DB (WAL +
		// busy_timeout), matching the existing concurrency tests.
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "claim.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() {
		Expect(s.Close()).To(Succeed())
	})

	It("claims an unclaimed node and stamps the key and time", func() {
		register("g1", "n1")

		got, err := s.ClaimNode(ctx, "g1", "machine-a")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
		Expect(got.ClaimKey).NotTo(BeNil())
		Expect(*got.ClaimKey).To(Equal("machine-a"))
		Expect(got.ClaimedAt).NotTo(BeNil())
	})

	It("is idempotent: the same key re-claims the same node, not a second one", func() {
		register("g1", "n1")
		register("g1", "n2")

		first, err := s.ClaimNode(ctx, "g1", "machine-a")
		Expect(err).NotTo(HaveOccurred())

		second, err := s.ClaimNode(ctx, "g1", "machine-a")
		Expect(err).NotTo(HaveOccurred())
		Expect(second.ID).To(Equal(first.ID), "replaying a claim must return the same node")

		// The other node must still be free.
		nodes, err := s.ListByGroup(ctx, "g1")
		Expect(err).NotTo(HaveOccurred())
		claimed := 0
		for _, n := range nodes {
			if n.ClaimKey != nil {
				claimed++
			}
		}
		Expect(claimed).To(Equal(1))
	})

	It("gives distinct keys distinct nodes", func() {
		register("g1", "n1")
		register("g1", "n2")

		a, err := s.ClaimNode(ctx, "g1", "machine-a")
		Expect(err).NotTo(HaveOccurred())
		b, err := s.ClaimNode(ctx, "g1", "machine-b")
		Expect(err).NotTo(HaveOccurred())
		Expect(a.ID).NotTo(Equal(b.ID))
	})

	It("returns ErrNoClaimCapacity when the group is fully claimed", func() {
		register("g1", "n1")
		_, err := s.ClaimNode(ctx, "g1", "machine-a")
		Expect(err).NotTo(HaveOccurred())

		_, err = s.ClaimNode(ctx, "g1", "machine-b")
		Expect(errors.Is(err, store.ErrNoClaimCapacity)).To(BeTrue())
	})

	It("returns ErrNoClaimCapacity for an empty or unknown group", func() {
		_, err := s.ClaimNode(ctx, "does-not-exist", "machine-a")
		Expect(errors.Is(err, store.ErrNoClaimCapacity)).To(BeTrue())
	})

	It("does not claim a node from a different group", func() {
		register("g1", "n1")
		_, err := s.ClaimNode(ctx, "g2", "machine-a")
		Expect(errors.Is(err, store.ErrNoClaimCapacity)).To(BeTrue())
	})

	Describe("release", func() {
		It("frees a node so it can be claimed again", func() {
			register("g1", "n1")
			claimed, err := s.ClaimNode(ctx, "g1", "machine-a")
			Expect(err).NotTo(HaveOccurred())

			released, err := s.ReleaseNode(ctx, claimed.ID, "machine-a")
			Expect(err).NotTo(HaveOccurred())
			Expect(released).To(BeTrue())

			// Now free: a re-read shows no claim, and a fresh claim picks it.
			reread, err := s.NodeGetByID(ctx, claimed.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(reread.ClaimKey).To(BeNil())
			Expect(reread.ClaimedAt).To(BeNil())

			again, err := s.ClaimNode(ctx, "g1", "machine-b")
			Expect(err).NotTo(HaveOccurred())
			Expect(again.ID).To(Equal(claimed.ID))
			Expect(*again.ClaimKey).To(Equal("machine-b"))
		})

		It("does not release a claim held by a different key", func() {
			register("g1", "n1")
			claimed, err := s.ClaimNode(ctx, "g1", "machine-a")
			Expect(err).NotTo(HaveOccurred())

			released, err := s.ReleaseNode(ctx, claimed.ID, "machine-b")
			Expect(err).NotTo(HaveOccurred())
			Expect(released).To(BeFalse())

			// Still owned by machine-a.
			reread, err := s.NodeGetByID(ctx, claimed.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(reread.ClaimKey).NotTo(BeNil())
			Expect(*reread.ClaimKey).To(Equal("machine-a"))
		})

		It("reports false when releasing an unclaimed node", func() {
			n := register("g1", "n1")
			released, err := s.ReleaseNode(ctx, n.ID, "machine-a")
			Expect(err).NotTo(HaveOccurred())
			Expect(released).To(BeFalse())
		})
	})

	It("gives every distinct key a distinct node under concurrent claims", func() {
		const nodes = 5
		const claimers = 16 // more claimers than nodes
		for i := 0; i < nodes; i++ {
			register("g1", fmt.Sprintf("node-%d", i))
		}

		type result struct {
			nodeID string
			err    error
		}
		results := make(chan result, claimers)
		var wg sync.WaitGroup
		for i := 0; i < claimers; i++ {
			wg.Add(1)
			go func(idx int) {
				defer GinkgoRecover()
				defer wg.Done()
				got, err := s.ClaimNode(ctx, "g1", fmt.Sprintf("machine-%d", idx))
				if err != nil {
					results <- result{err: err}
					return
				}
				results <- result{nodeID: got.ID}
			}(i)
		}
		wg.Wait()
		close(results)

		claimedIDs := map[string]int{}
		noCapacity := 0
		for r := range results {
			if r.err != nil {
				Expect(errors.Is(r.err, store.ErrNoClaimCapacity)).To(BeTrue(),
					"the only acceptable error is no-capacity, got: %v", r.err)
				noCapacity++
				continue
			}
			claimedIDs[r.nodeID]++
		}
		// Exactly `nodes` distinct nodes were claimed, each by exactly one winner,
		// and every other claimer got a clean no-capacity signal.
		Expect(claimedIDs).To(HaveLen(nodes))
		for id, n := range claimedIDs {
			Expect(n).To(Equal(1), "node %s was handed to more than one claimer", id)
		}
		Expect(noCapacity).To(Equal(claimers - nodes))
	})

	It("hands the same node to every concurrent claim that shares a key", func() {
		const nodes = 5
		const claimers = 16
		for i := 0; i < nodes; i++ {
			register("g1", fmt.Sprintf("node-%d", i))
		}

		results := make(chan string, claimers)
		errs := make(chan error, claimers)
		var wg sync.WaitGroup
		for i := 0; i < claimers; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				got, err := s.ClaimNode(ctx, "g1", "same-key")
				if err != nil {
					errs <- err
					return
				}
				results <- got.ID
			}()
		}
		wg.Wait()
		close(results)
		close(errs)

		for err := range errs {
			Expect(err).NotTo(HaveOccurred())
		}
		ids := map[string]struct{}{}
		count := 0
		for id := range results {
			ids[id] = struct{}{}
			count++
		}
		Expect(count).To(Equal(claimers))
		Expect(ids).To(HaveLen(1), "a single claimKey must never own more than one node")

		// And only that one node is claimed in the group.
		grp, err := s.ListByGroup(ctx, "g1")
		Expect(err).NotTo(HaveOccurred())
		claimed := 0
		for _, n := range grp {
			if n.ClaimKey != nil {
				claimed++
			}
		}
		Expect(claimed).To(Equal(1))
	})
})
