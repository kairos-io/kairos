package state

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// stubDisks answers the block-device lookups selectTargetDisk makes.
// candidates is in the order CandidateDisks returns, largest first.
// withPartitions and withKairosPartitions are sets of device paths.
func stubDisks(candidates []string, withPartitions, withKairosPartitions []string, wipe bool) func() {
	previousCandidates := candidateDisksFn
	previousHasPartitions := diskHasPartitionsFn
	previousHasKairos := diskHasKairosPartitionsFn
	previousWipe := autoCreateWipeEnabledFn

	in := func(set []string, path string) bool {
		for _, s := range set {
			if s == path {
				return true
			}
		}
		return false
	}

	candidateDisksFn = func() []string { return candidates }
	diskHasPartitionsFn = func(path string) bool { return in(withPartitions, path) }
	diskHasKairosPartitionsFn = func(path string) bool { return in(withKairosPartitions, path) }
	autoCreateWipeEnabledFn = func() bool { return wipe }

	return func() {
		candidateDisksFn = previousCandidates
		diskHasPartitionsFn = previousHasPartitions
		diskHasKairosPartitionsFn = previousHasKairos
		autoCreateWipeEnabledFn = previousWipe
	}
}

var _ = Describe("selectTargetDisk", func() {
	var restore func()

	AfterEach(func() {
		if restore != nil {
			restore()
			restore = nil
		}
	})

	It("takes the disk the operator named", func() {
		restore = stubDisks([]string{"/dev/sda"}, nil, nil, false)
		previousExists := diskExistsFn
		diskExistsFn = func(string) bool { return true }
		defer func() { diskExistsFn = previousExists }()

		Expect(selectTargetDisk("/dev/sdb", false)).To(Equal("/dev/sdb"))
	})

	It("takes the largest empty disk when nothing carries our labels", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sda"},
			nil,
			false,
		)
		Expect(selectTargetDisk("", false)).To(Equal("/dev/sdb"))
	})

	It("takes the largest disk overall when every disk is in use", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sda", "/dev/sdb"},
			nil,
			false,
		)
		Expect(selectTargetDisk("", false)).To(Equal("/dev/sda"))
	})

	// The partial case: COS_OEM is on the smaller disk and the missing
	// COS_PERSISTENT belongs next to it. Picking the larger empty disk would
	// send init_disk: false to a disk with no partition table.
	It("takes the disk that already carries a Kairos partition", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sdb"},
			[]string{"/dev/sdb"},
			false,
		)
		Expect(selectTargetDisk("", true)).To(Equal("/dev/sdb"))
	})

	It("takes the disk that already carries a Kairos partition with the wipe flag too", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sdb"},
			[]string{"/dev/sdb"},
			true,
		)
		Expect(selectTargetDisk("", true)).To(Equal("/dev/sdb"))
	})

	It("takes the largest disk with the wipe flag when nothing carries our labels", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sda"},
			nil,
			true,
		)
		Expect(selectTargetDisk("", false)).To(Equal("/dev/sda"))
	})

	// The encrypted post-#4403 host: /dev/sdb carries COS_OEM_LUKS, which
	// DiskHasKairosPartitions matches and KairosPartitionsPresent does not.
	// The caller therefore sees both labels missing (partial false) and the
	// preference loop must stay out of the way, or the boot lands on the live
	// encrypted install instead of the empty disk.
	It("leaves an encrypted disk alone when the caller sees both labels missing", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sdb"},
			[]string{"/dev/sdb"},
			false,
		)
		Expect(selectTargetDisk("", false)).To(Equal("/dev/sda"))
	})
})

var _ = Describe("resolveInitDisk", func() {
	var restore func()

	AfterEach(func() {
		if restore != nil {
			restore()
			restore = nil
		}
	})

	It("writes a fresh GPT when both labels are missing and the disk is empty", func() {
		restore = stubDisks([]string{"/dev/sda"}, nil, nil, false)
		Expect(resolveInitDisk(true, "/dev/sda")).To(BeTrue())
	})

	// The other half of #4804: init_disk must follow the disk we resolved,
	// not the whole-machine scan. A disk with a partition table has nothing
	// to gain from a fresh GPT and everything to lose.
	It("appends when both labels are missing but the target has a partition table", func() {
		restore = stubDisks([]string{"/dev/sda"}, []string{"/dev/sda"}, nil, false)
		Expect(resolveInitDisk(true, "/dev/sda")).To(BeFalse())
	})

	It("writes a fresh GPT over a used disk once the operator asked for a wipe", func() {
		restore = stubDisks([]string{"/dev/sda"}, []string{"/dev/sda"}, nil, true)
		Expect(resolveInitDisk(true, "/dev/sda")).To(BeTrue())
	})

	It("always appends when one of the labels is already present", func() {
		restore = stubDisks([]string{"/dev/sda"}, nil, nil, true)
		Expect(resolveInitDisk(false, "/dev/sda")).To(BeFalse())
	})
})
