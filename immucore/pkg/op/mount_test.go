package op

import (
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A device that cannot exist, so every mount attempt fails and the call runs
// until the timeout instead of succeeding on some attempt.
const missingDevice = "/dev/immucore-does-not-exist"

// One attempt against missingDevice is not cheap: DiskFSType forks blkid,
// which takes seconds on some machines. So no spec here bounds the elapsed
// time against the timeout. Instead they stretch mountRetryInterval far past
// any plausible attempt cost, which makes "did it wait an interval?" readable
// from the clock with a wide margin.
const stretchedInterval = 60 * time.Second

// stretchRetryInterval sets mountRetryInterval for the current spec only.
func stretchRetryInterval() {
	previous := mountRetryInterval
	mountRetryInterval = stretchedInterval
	DeferCleanup(func() { mountRetryInterval = previous })
}

var _ = Describe("MountOPWithFstab", func() {
	var target string

	BeforeEach(func() {
		target = filepath.Join(GinkgoT().TempDir(), "target")
	})

	It("attempts the mount before waiting, so a timeout under the retry interval still tries once", func(_ SpecContext) {
		stretchRetryInterval()

		_, err := MountOPWithFstab(missingDevice, target, "ext4", []string{"ro"}, 50*time.Millisecond)

		Expect(err).To(MatchError(ContainSubstring("timeout exhausted")))
		// The first attempt has to have run: it is what creates the target
		// dir. Waiting first would have burned the whole timeout instead.
		Expect(target).To(BeADirectory())
	}, NodeTimeout(stretchedInterval/2))

	It("lets the timeout cut the retry wait short", func(_ SpecContext) {
		stretchRetryInterval()

		start := time.Now()
		fstab, err := MountOPWithFstab(missingDevice, target, "ext4", []string{"ro"}, 50*time.Millisecond)
		elapsed := time.Since(start)

		Expect(err).To(MatchError(ContainSubstring("timeout exhausted")))
		Expect(fstab).To(BeEmpty())
		// The wait has to be a select case the timeout can interrupt. An
		// uninterruptible one cannot return before a full interval, so any
		// bound comfortably under it catches that.
		Expect(elapsed).To(BeNumerically("<", stretchedInterval/3))
	}, NodeTimeout(stretchedInterval/2))

	It("keeps retrying for the whole timeout instead of giving up on the first failure", func(_ SpecContext) {
		const timeout = 300 * time.Millisecond

		start := time.Now()
		_, err := MountOPWithFstab(missingDevice, target, "ext4", []string{"ro"}, timeout)
		elapsed := time.Since(start)

		Expect(err).To(MatchError(ContainSubstring("timeout exhausted")))
		Expect(elapsed).To(BeNumerically(">=", timeout))
	}, NodeTimeout(time.Minute))
})
