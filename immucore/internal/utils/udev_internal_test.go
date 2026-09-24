package utils

import (
	"os"
	"time"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/kairos-io/kairos/v4/immucore/tests/mocks"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("waitForLabels", func() {
	// present answers "not yet" for the first notBefore calls and "yes"
	// afterwards, so a test can say exactly how late udev is.
	present := func(notBefore int, calls *int) func(...string) bool {
		return func(...string) bool {
			*calls++
			return *calls > notBefore
		}
	}

	It("returns at once when the label is already enumerated", func() {
		calls := 0
		start := time.Now()
		Expect(waitForLabels([]string{"COS_STATE"}, time.Minute, time.Minute, present(0, &calls))).To(Succeed())
		Expect(calls).To(Equal(1))
		// A device that is already there must not pay one poll interval.
		Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))
	})

	It("succeeds once a late device shows up", func() {
		calls := 0
		Expect(waitForLabels([]string{"COS_STATE"}, time.Second, time.Millisecond, present(3, &calls))).To(Succeed())
		Expect(calls).To(Equal(4))
	})

	It("reports the label and the budget when the device never shows up", func() {
		calls := 0
		err := waitForLabels([]string{"COS_RECOVERY"}, 10*time.Millisecond, time.Millisecond, present(1<<30, &calls))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"COS_RECOVERY"`))
		Expect(err.Error()).To(ContainSubstring("10ms"))
		Expect(calls).To(BeNumerically(">", 1))
	})

	// A whole set is scanned in one pass, so waiting on the three OEM labels
	// costs one device scan per poll and not three.
	It("scans the whole set once per poll", func() {
		var seen [][]string
		err := waitForLabels(
			[]string{"COS_OEM", "COS_OEM_LUKS", "oem"},
			2*time.Millisecond, time.Millisecond,
			func(labels ...string) bool { seen = append(seen, labels); return false },
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("none of the labels"))
		for _, labels := range seen {
			Expect(labels).To(Equal([]string{"COS_OEM", "COS_OEM_LUKS", "oem"}))
		}
	})

	// A zero timeout is the "check, do not wait" case. It still has to answer
	// correctly for a device that is already enumerated, which it only does
	// because the scan runs before the deadline is consulted.
	It("still finds an enumerated device with a zero timeout", func() {
		calls := 0
		Expect(waitForLabels([]string{"COS_STATE"}, 0, time.Second, present(0, &calls))).To(Succeed())
		Expect(calls).To(Equal(1))
	})

	It("does not wait with a zero timeout when the device is absent", func() {
		calls := 0
		start := time.Now()
		Expect(waitForLabels([]string{"COS_STATE"}, 0, time.Hour, present(1<<30, &calls))).ToNot(Succeed())
		Expect(calls).To(Equal(1))
		Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))
	})
})

var _ = Describe("waitForBootDevices", func() {
	// asked records every set of labels the wait scanned for, in order, so a
	// spec can say which legs ran.
	asked := func(found map[string]bool, seen *[][]string) func(...string) bool {
		return func(labels ...string) bool {
			*seen = append(*seen, labels)
			for _, l := range labels {
				if found[l] {
					return true
				}
			}
			return false
		}
	}

	// LiveCD has no images partition, and the OEM leg only makes sense once
	// the disk is known to be awake. Both have to be a no-op, or every live
	// boot pays the budget for devices that are not there.
	It("does not wait at all on a LiveCD boot", func() {
		var seen [][]string
		start := time.Now()
		Expect(waitForBootDevices(state.LiveCD, time.Hour, time.Hour, time.Hour, asked(nil, &seen))).To(Succeed())
		Expect(seen).To(BeEmpty())
		Expect(time.Since(start)).To(BeNumerically("<", 500*time.Millisecond))
	})

	// An unclassifiable boot is the other silent no-wait path: DetectBoot
	// returns Unknown when it cannot read the cmdline.
	It("does not wait at all when the boot state is unknown", func() {
		var seen [][]string
		Expect(waitForBootDevices(state.Unknown, time.Hour, time.Hour, time.Hour, asked(nil, &seen))).To(Succeed())
		Expect(seen).To(BeEmpty())
	})

	It("waits for the images partition and then for OEM", func() {
		var seen [][]string
		found := map[string]bool{"COS_STATE": true, sdkConstants.OEMLabel: true}
		Expect(waitForBootDevices(state.Active, time.Second, time.Second, time.Millisecond, asked(found, &seen))).To(Succeed())
		Expect(seen).To(HaveLen(2))
		Expect(seen[0]).To(Equal([]string{"COS_STATE"}))
		Expect(seen[1]).To(ConsistOf(sdkConstants.OEMLabel, sdkConstants.OEMLUKSLabel, sdkConstants.OEMPartName))
	})

	It("waits on the recovery partition when recovery is booting", func() {
		var seen [][]string
		found := map[string]bool{"COS_RECOVERY": true, sdkConstants.OEMLUKSLabel: true}
		Expect(waitForBootDevices(state.Recovery, time.Second, time.Second, time.Millisecond, asked(found, &seen))).To(Succeed())
		Expect(seen[0]).To(Equal([]string{"COS_RECOVERY"}))
	})

	// The OEM leg is the whole point of the wait: oemEncrypted() reads an
	// empty scan as "not encrypted" and wires the DAG to mount a LUKS
	// container as ext4, so the encrypted label has to settle it too.
	It("settles on the LUKS OEM label", func() {
		var seen [][]string
		found := map[string]bool{"COS_STATE": true, sdkConstants.OEMLUKSLabel: true}
		Expect(waitForBootDevices(state.Active, time.Second, time.Second, time.Millisecond, asked(found, &seen))).To(Succeed())
		Expect(seen).To(HaveLen(2))
	})

	// An installation is allowed to carry no OEM partition. That boot must
	// still succeed, and it must not be reported as a failure upstream.
	It("succeeds when no OEM partition ever appears", func() {
		var seen [][]string
		found := map[string]bool{"COS_STATE": true}
		Expect(waitForBootDevices(state.Active, time.Second, 5*time.Millisecond, time.Millisecond, asked(found, &seen))).To(Succeed())
		Expect(len(seen)).To(BeNumerically(">", 2))
	})

	// A missing images partition is the one leg that is reported, because
	// there is nothing sensible to boot without it.
	It("reports the images partition it never saw, and skips the OEM leg", func() {
		var seen [][]string
		err := waitForBootDevices(state.Active, 5*time.Millisecond, time.Hour, time.Millisecond, asked(nil, &seen))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(`"COS_STATE"`))
		for _, labels := range seen {
			Expect(labels).To(Equal([]string{"COS_STATE"}))
		}
	})
})

var _ = Describe("oemLabelsToWaitFor", func() {
	var cmdline string

	BeforeEach(func() {
		f, err := os.CreateTemp("", "cmdline")
		Expect(err).ToNot(HaveOccurred())
		cmdline = f.Name()
		Expect(f.Close()).To(Succeed())
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
		Expect(os.Remove(cmdline)).To(Succeed())
	})

	It("falls back to every label GetOemLabel scans for", func() {
		Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_STATE\n"), 0644)).To(Succeed())
		Expect(oemLabelsToWaitFor()).To(Equal([]string{
			sdkConstants.OEMLabel, sdkConstants.OEMLUKSLabel, sdkConstants.OEMPartName,
		}))
	})

	// When the cmdline names the label, that single label is what
	// GetOemLabel returns, so waiting for the defaults would wait for
	// partitions this installation does not have.
	It("waits only for the label the cmdline names", func() {
		Expect(os.WriteFile(cmdline, []byte("rd.immucore.oemlabel=MY_OEM\n"), 0644)).To(Succeed())
		Expect(oemLabelsToWaitFor()).To(Equal([]string{"MY_OEM"}))
	})
})

var _ = Describe("labelsEnumerated", func() {
	var ghwTest mocks.GhwMock

	BeforeEach(func() {
		// The immucore mock writes the struct's Label into E:ID_FS_LABEL and
		// its FilesystemLabel into E:ID_PART_ENTRY_NAME, which is what
		// sdk/ghw reads back as FilesystemLabel and PartitionLabel
		// respectively.
		ghwTest = mocks.GhwMock{}
		ghwTest.AddDisk(block.Disk{
			Name: "device",
			Partitions: []*block.Partition{
				{Name: "device1", Label: "COS_STATE", FilesystemLabel: "state", Type: "ext4"},
				{Name: "device2", Label: "", FilesystemLabel: sdkConstants.OEMPartName, Type: "ext4"},
			},
		})
		ghwTest.CreateDevices()
	})

	AfterEach(func() {
		ghwTest.Clean()
	})

	It("sees a partition by its filesystem label", func() {
		Expect(labelsEnumerated("COS_STATE")).To(BeTrue())
	})

	It("sees a partition by its GPT partition label", func() {
		Expect(labelsEnumerated(sdkConstants.OEMPartName)).To(BeTrue())
	})

	It("does not see a label no partition carries", func() {
		Expect(labelsEnumerated("COS_RECOVERY")).To(BeFalse())
	})

	It("is true when any label in the set is carried", func() {
		Expect(labelsEnumerated(sdkConstants.OEMLabel, sdkConstants.OEMLUKSLabel, sdkConstants.OEMPartName)).To(BeTrue())
	})

	It("is false when no label in the set is carried", func() {
		Expect(labelsEnumerated(sdkConstants.OEMLabel, sdkConstants.OEMLUKSLabel)).To(BeFalse())
	})
})
