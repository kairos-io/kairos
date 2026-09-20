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

		Expect(selectTargetDisk("/dev/sdb")).To(Equal("/dev/sdb"))
	})

	It("takes the largest empty disk when nothing carries our labels", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sda"},
			nil,
			false,
		)
		Expect(selectTargetDisk("")).To(Equal("/dev/sdb"))
	})

	It("takes the largest disk overall when every disk is in use", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sda", "/dev/sdb"},
			nil,
			false,
		)
		Expect(selectTargetDisk("")).To(Equal("/dev/sda"))
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
		Expect(selectTargetDisk("")).To(Equal("/dev/sdb"))
	})

	It("takes the disk that already carries a Kairos partition with the wipe flag too", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sdb"},
			[]string{"/dev/sdb"},
			true,
		)
		Expect(selectTargetDisk("")).To(Equal("/dev/sdb"))
	})

	It("takes the largest disk with the wipe flag when nothing carries our labels", func() {
		restore = stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sda"},
			nil,
			true,
		)
		Expect(selectTargetDisk("")).To(Equal("/dev/sda"))
	})
})
