package op_test

import (
	"path/filepath"
	"time"

	"github.com/kairos-io/kairos/v4/immucore/pkg/op"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MountOPWithFstab", func() {
	// A device that cannot exist, so every mount attempt fails and the call
	// runs until the timeout instead of succeeding on some attempt.
	const missingDevice = "/dev/immucore-does-not-exist"

	It("gives up at the timeout instead of overrunning it by the retry wait", func() {
		target := filepath.Join(GinkgoT().TempDir(), "target")

		start := time.Now()
		fstab, err := op.MountOPWithFstab(missingDevice, target, "ext4", []string{"ro"}, 50*time.Millisecond)
		elapsed := time.Since(start)

		Expect(err).To(MatchError(ContainSubstring("timeout exhausted")))
		Expect(fstab).To(BeEmpty())
		// The retry wait has to be interruptible by the timeout, so the call
		// returns at its 50ms deadline and not one retry interval later. The
		// timeout is deliberately shorter than the interval: an uninterruptible
		// wait cannot return before 250ms, which is what this bound catches.
		Expect(elapsed).To(BeNumerically("<", 200*time.Millisecond))
	})

	It("attempts the mount before waiting, so a timeout under the retry interval still tries once", func() {
		target := filepath.Join(GinkgoT().TempDir(), "target")

		_, err := op.MountOPWithFstab(missingDevice, target, "ext4", []string{"ro"}, 50*time.Millisecond)

		Expect(err).To(HaveOccurred())
		// The first attempt has to have run: it is what creates the target dir.
		Expect(target).To(BeADirectory())
	})

	It("paces retries rather than spinning on them", func() {
		target := filepath.Join(GinkgoT().TempDir(), "target")

		start := time.Now()
		_, err := op.MountOPWithFstab(missingDevice, target, "ext4", []string{"ro"}, 1500*time.Millisecond)
		elapsed := time.Since(start)

		Expect(err).To(MatchError(ContainSubstring("timeout exhausted")))
		// Failing attempts are cheap, so without a wait between them this would
		// return as soon as the timeout fires having burned the CPU throughout.
		// With the wait it also returns at the timeout, but the guard here is
		// that the loop cannot finish early by racing through attempts.
		Expect(elapsed).To(BeNumerically(">=", 1400*time.Millisecond))
	})
})
