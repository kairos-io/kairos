package gorm_test

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("Gorm Store reset lifecycle", func() {
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
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "reset.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	It("issuing a reset marks the node pending and stamps the request time", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)

		got, err := s.NodeGetByID(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.ResetState).To(Equal(store.ResetStatePending))
		Expect(got.ResetRequestedAt).NotTo(BeNil())
		Expect(got.LastReset).To(BeNil())
	})

	It("advances pending -> in-progress without stamping LastReset", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)

		ok, err := s.AdvanceReset(ctx, n.ID, []string{store.ResetStatePending}, store.ResetStateInProgress, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateInProgress))
		Expect(got.LastReset).To(BeNil())
		// Reaching in-progress means the node re-registered, so it re-anchors the
		// expiry budget (kairos-io/kairos#4287).
		Expect(got.ResetProgressAt).NotTo(BeNil())
	})

	It("stamps ResetProgressAt when a heartbeat after the reboot reports autoreset", func() {
		n := register("n1")
		cmdID := issueCommand(ctx, s, n.ID, store.CmdReset)
		Expect(s.UpdateStatus(ctx, cmdID, store.CommandCompleted, "")).To(Succeed())

		before := time.Now()
		Expect(s.SettleAfterReboot(ctx, n.ID, store.BootStateAutoReset, time.Now().Add(time.Second))).To(Succeed())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateInProgress))
		// An agent with credentials heartbeats instead of re-registering, so this
		// path has to re-anchor the expiry budget too (kairos-io/kairos#4287).
		Expect(got.ResetProgressAt).NotTo(BeNil())
		Expect(got.ResetProgressAt.Before(before)).To(BeFalse())
	})

	It("does not stamp ResetProgressAt on the terminal transitions", func() {
		for _, to := range []string{store.ResetStateDone, store.ResetStateFailed} {
			n := register("n-" + to)
			issueCommand(ctx, s, n.ID, store.CmdReset)

			ok, err := s.AdvanceReset(ctx, n.ID,
				[]string{store.ResetStatePending, store.ResetStateInProgress}, to, to == store.ResetStateDone)
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			got, _ := s.NodeGetByID(ctx, n.ID)
			Expect(got.ResetProgressAt).To(BeNil(), "transition to %s", to)
		}
	})

	// The reset timeout must measure from the last proof the node is alive, not
	// from the request, or a node that is actively wiping gets reported failed.
	It("does not fail a reset whose node re-registered after the deadline", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		requested, err := s.NodeGetByID(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		deadline := requested.ResetRequestedAt.Add(time.Millisecond)

		// The node comes back mid-reset, after the original window closed.
		time.Sleep(2 * time.Millisecond)
		ok, err := s.AdvanceReset(ctx, n.ID, []string{store.ResetStatePending}, store.ResetStateInProgress, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		matched, err := s.FailResetBefore(ctx, n.ID, deadline)
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeFalse())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateInProgress))
	})

	It("fails a reset whose re-register is itself older than the deadline", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		ok, err := s.AdvanceReset(ctx, n.ID, []string{store.ResetStatePending}, store.ResetStateInProgress, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		matched, err := s.FailResetBefore(ctx, n.ID, time.Now().Add(time.Minute))
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateFailed))
	})

	// CreateBatch leaves a reset that is still pending or in-progress alone, so
	// the stamp can only be inherited across a reset that already resolved.
	It("clears ResetProgressAt when the next reset is requested", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		_, err := s.AdvanceReset(ctx, n.ID, []string{store.ResetStatePending}, store.ResetStateInProgress, false)
		Expect(err).NotTo(HaveOccurred())
		mid, _ := s.NodeGetByID(ctx, n.ID)
		Expect(mid.ResetProgressAt).NotTo(BeNil())

		_, err = s.AdvanceReset(ctx, n.ID, []string{store.ResetStateInProgress}, store.ResetStateDone, true)
		Expect(err).NotTo(HaveOccurred())

		issueCommand(ctx, s, n.ID, store.CmdReset)
		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStatePending))
		Expect(got.ResetProgressAt).To(BeNil())
	})

	It("advances an in-flight reset -> done and stamps LastReset", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)

		ok, err := s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending, store.ResetStateInProgress}, store.ResetStateDone, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateDone))
		Expect(got.LastReset).NotTo(BeNil())
	})

	It("advances an in-flight reset -> failed", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)

		ok, err := s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending, store.ResetStateInProgress}, store.ResetStateFailed, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateFailed))
		Expect(got.LastReset).To(BeNil())
	})

	// The request time can only move while a reset is in flight if the first one
	// resolved first: CreateBatch leaves a pending or in-progress reset alone.
	It("does not fail a reset whose request timestamp was refreshed", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		first, err := s.NodeGetByID(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		oldDeadline := *first.ResetRequestedAt

		_, err = s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending}, store.ResetStateFailed, false)
		Expect(err).NotTo(HaveOccurred())

		time.Sleep(time.Millisecond)
		issueCommand(ctx, s, n.ID, store.CmdReset)
		matched, err := s.FailResetBefore(ctx, n.ID, oldDeadline)
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeFalse())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStatePending))
		Expect(got.ResetRequestedAt.After(oldDeadline)).To(BeTrue())
	})

	It("fails an in-flight reset requested before the deadline", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)

		matched, err := s.FailResetBefore(ctx, n.ID, time.Now().Add(time.Minute))
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStateFailed))
	})

	It("does not transition when the current state is not in fromStates", func() {
		n := register("n1") // ResetState == "" (no reset requested)
		ok, err := s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending, store.ResetStateInProgress}, store.ResetStateDone, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(""))
		Expect(got.LastReset).To(BeNil())
	})

	It("resolves exactly once under concurrent AdvanceReset", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)

		const workers = 12
		wins := make(chan bool, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer GinkgoRecover()
				defer wg.Done()
				ok, err := s.AdvanceReset(ctx, n.ID,
					[]string{store.ResetStatePending, store.ResetStateInProgress}, store.ResetStateDone, true)
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

var _ = Describe("Gorm Store StampResetProgress", func() {
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
		s, err = gormstore.New(filepath.Join(GinkgoT().TempDir(), "stamp.db"))
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	AfterEach(func() { Expect(s.Close()).To(Succeed()) })

	It("stamps a pending reset and re-anchors the expiry budget", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		// The expiry sweep passes now-timeout, so the deadline it compares
		// against is always older than a delivery that has just happened.
		deadline := time.Now()
		time.Sleep(10 * time.Millisecond)

		stamped, err := s.StampResetProgress(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stamped).To(BeTrue())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetProgressAt).NotTo(BeNil())
		Expect(got.ResetState).To(Equal(store.ResetStatePending),
			"taking delivery must not advance the lifecycle on its own")

		// The whole point: a node that only now took delivery must survive the
		// expiry sweep that the original request timestamp would have lost.
		matched, err := s.FailResetBefore(ctx, n.ID, deadline)
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeFalse())
		got, _ = s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetState).To(Equal(store.ResetStatePending))
	})

	It("stamps an in-progress reset", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		_, err := s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending}, store.ResetStateInProgress, false)
		Expect(err).NotTo(HaveOccurred())

		stamped, err := s.StampResetProgress(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stamped).To(BeTrue())
	})

	It("refuses to stamp a reset that already resolved", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		_, err := s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending}, store.ResetStateDone, true)
		Expect(err).NotTo(HaveOccurred())

		stamped, err := s.StampResetProgress(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stamped).To(BeFalse())

		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetProgressAt).To(BeNil())
		Expect(got.ResetState).To(Equal(store.ResetStateDone))
	})

	It("refuses to stamp a node that has never been reset", func() {
		n := register("n1")
		stamped, err := s.StampResetProgress(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(stamped).To(BeFalse())
	})

	It("clears the delivery stamp when the next reset is requested", func() {
		n := register("n1")
		issueCommand(ctx, s, n.ID, store.CmdReset)
		_, err := s.StampResetProgress(ctx, n.ID)
		Expect(err).NotTo(HaveOccurred())

		// CreateBatch keeps a reset that is still in flight, so the stamp can
		// only survive into a reset issued after this one resolved.
		_, err = s.AdvanceReset(ctx, n.ID,
			[]string{store.ResetStatePending}, store.ResetStateFailed, false)
		Expect(err).NotTo(HaveOccurred())

		issueCommand(ctx, s, n.ID, store.CmdReset)
		got, _ := s.NodeGetByID(ctx, n.ID)
		Expect(got.ResetProgressAt).To(BeNil(),
			"a new reset must not inherit the previous delivery stamp")
	})
})
