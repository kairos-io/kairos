package utils

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("waitForLabel", func() {
	// present answers "not yet" for the first notBefore calls and "yes"
	// afterwards, so a test can say exactly how late udev is.
	present := func(notBefore int, calls *int) func(string) bool {
		return func(string) bool {
			*calls++
			return *calls > notBefore
		}
	}

	It("returns at once when the label is already enumerated", func() {
		calls := 0
		start := time.Now()
		Expect(waitForLabel("COS_STATE", time.Minute, time.Minute, present(0, &calls))).To(Succeed())
		Expect(calls).To(Equal(1))
		// A device that is already there must not pay one poll interval.
		Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))
	})

	It("succeeds once a late device shows up", func() {
		calls := 0
		Expect(waitForLabel("COS_STATE", time.Second, time.Millisecond, present(3, &calls))).To(Succeed())
		Expect(calls).To(Equal(4))
	})

	It("reports the label and the budget when the device never shows up", func() {
		calls := 0
		err := waitForLabel("COS_RECOVERY", 10*time.Millisecond, time.Millisecond, present(1<<30, &calls))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"COS_RECOVERY"`))
		Expect(err.Error()).To(ContainSubstring("10ms"))
		Expect(calls).To(BeNumerically(">", 1))
	})

	// A zero timeout is the "check, do not wait" case. It still has to answer
	// correctly for a device that is already enumerated, which it only does
	// because the scan runs before the deadline is consulted.
	It("still finds an enumerated device with a zero timeout", func() {
		calls := 0
		Expect(waitForLabel("COS_STATE", 0, time.Second, present(0, &calls))).To(Succeed())
		Expect(calls).To(Equal(1))
	})

	It("does not wait with a zero timeout when the device is absent", func() {
		calls := 0
		start := time.Now()
		Expect(waitForLabel("COS_STATE", 0, time.Hour, present(1<<30, &calls))).ToNot(Succeed())
		Expect(calls).To(Equal(1))
		Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))
	})
})
