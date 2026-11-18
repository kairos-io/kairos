package gorm_test

import (
	"context"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm Store upgrade lifecycle", func() {
	var (
		s   *gormstore.Store
		ctx context.Context
	)

	register := func(machineID string) *store.ManagedNode {
		n := &store.ManagedNode{MachineID: machineID}
		Expect(s.Register(ctx, n)).To(Succeed())
		return n
	}

	BeforeEach(func() {
		var err error
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "upgrade.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	It("issuing an upgrade marks the node pending and stamps the request time", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)

		got, err := s.NodeGetByID(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
		Expect(got.UpgradeRequestedAt).NotTo(BeNil())
		Expect(got.LastUpgrade).To(BeNil())
	})

	It("issuing an upgrade leaves the reset lifecycle alone", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStatePending))
		Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("re-issuing an upgrade after a finished one moves it back to pending", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)
		ok, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateFailed, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		issueCommand(ctx, s, n.ID, store.CmdUpgrade)
		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.UpgradeState).To(Equal(store.UpgradeStatePending))
	})

	It("advances pending -> done and stamps LastUpgrade", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)

		ok, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateDone, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.UpgradeState).To(Equal(store.UpgradeStateDone))
		Expect(got.LastUpgrade).NotTo(BeNil())
	})

	It("advances pending -> failed without stamping LastUpgrade", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)

		ok, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateFailed, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.UpgradeState).To(Equal(store.UpgradeStateFailed))
		Expect(got.LastUpgrade).To(BeNil())
	})

	It("does not transition when the current state is not in fromStates", func() {
		n := register("n1") // UpgradeState == "" (no upgrade requested)
		ok, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateDone, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.UpgradeState).To(Equal(""))
		Expect(got.LastUpgrade).To(BeNil())
	})

	It("does not resolve an already finished upgrade a second time", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)
		ok, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateFailed, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		ok, err = s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateDone, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.UpgradeState).To(Equal(store.UpgradeStateFailed))
		Expect(got.LastUpgrade).To(BeNil())
	})

	It("resolves exactly once under concurrent AdvanceUpgrade", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdUpgrade)

		const workers = 12
		wins := make(chan bool, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				ok, err := s.AdvanceUpgrade(ctx, n.ID, []string{store.UpgradeStatePending}, store.UpgradeStateDone, true)
				Expect(err).NotTo(HaveOccurred())
				wins <- ok
			}()
		}
		wg.Wait()
		close(wins)

		won := 0
		for w := range wins {
			if w {
				won++
			}
		}
		Expect(won).To(Equal(1), "exactly one concurrent resolver must perform the transition")
	})
})
