package state

import (
	"fmt"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This file answers the review question on #4805 that a QEMU boot would
// otherwise have to answer: does gating the labelled-disk preference on
// `partial` keep every both-missing boot on the disk pre-#4804 master would
// have chosen? Booting is not available to the author, so the proof is an
// exhaustive one instead of an anecdotal one: it enumerates every machine
// this code can see and compares all three algorithms on each of them.
//
// baselineSelectTargetDisk is pre-#4804 master's selection, transcribed with
// the same seams the rest of this package's tests use. Its halt branches are
// left out: the enumeration never produces an empty candidate list or an
// explicit disk, which are the only paths that reach them, and both are
// covered by name in steps_ensure_partitions_internal_test.go.
func baselineSelectTargetDisk() string {
	candidates := candidateDisksFn()
	selected := candidates[0]
	if !autoCreateWipeEnabledFn() {
		for _, c := range candidates {
			if !diskHasPartitionsFn(c) {
				selected = c
				break
			}
		}
	}
	return selected
}

// mergedSelectTargetDisk is #4804 as it merged: the labelled-disk preference
// ran on every boot, which is the regression this pull request narrows.
func mergedSelectTargetDisk() string {
	selected := baselineSelectTargetDisk()
	for _, c := range candidateDisksFn() {
		if diskHasKairosPartitionsFn(c) {
			selected = c
			break
		}
	}
	return selected
}

// diskState is what the block-device lookups can report about one disk.
// "labelled" implies "partitioned": a disk carrying COS_OEM has a table.
type diskState int

const (
	diskEmpty diskState = iota
	diskInUse
	diskLabelled
)

func (d diskState) String() string {
	switch d {
	case diskEmpty:
		return "empty"
	case diskInUse:
		return "in-use"
	default:
		return "kairos-labelled"
	}
}

type scanState struct {
	oem        bool
	persistent bool
}

func (s scanState) String() string {
	switch {
	case s.oem && s.persistent:
		return "both-present"
	case s.oem:
		return "oem-only"
	case s.persistent:
		return "persistent-only"
	default:
		return "both-missing"
	}
}

func (s scanState) partial() bool { return s.oem != s.persistent }

var _ = Describe("selectTargetDisk equivalence with pre-#4804 master", func() {
	// EnsurePartitionsDagStep returns before selectTargetDisk when both
	// labels are present, so that scan state cannot reach this code.
	scans := []scanState{{}, {oem: true}, {persistent: true}}
	states := []diskState{diskEmpty, diskInUse, diskLabelled}

	// Every machine of one, two or three disks, every disk in every state,
	// with and without kairos.ram.wipe, under every scan state that reaches
	// the selection. Candidate order is largest-first, as CandidateDisks
	// returns it, so index 0 is the largest disk.
	type machine struct {
		name       string
		candidates []string
		disks      []diskState
		wipe       bool
		scan       scanState
	}
	var machines []machine
	for diskCount := 1; diskCount <= 3; diskCount++ {
		combos := [][]diskState{{}}
		for i := 0; i < diskCount; i++ {
			var next [][]diskState
			for _, combo := range combos {
				for _, s := range states {
					next = append(next, append(append([]diskState{}, combo...), s))
				}
			}
			combos = next
		}
		for _, combo := range combos {
			candidates := make([]string, diskCount)
			described := make([]string, diskCount)
			for i, s := range combo {
				candidates[i] = fmt.Sprintf("/dev/sd%c", 'a'+i)
				described[i] = fmt.Sprintf("%s=%s", candidates[i], s)
			}
			for _, wipe := range []bool{false, true} {
				for _, scan := range scans {
					machines = append(machines, machine{
						name: fmt.Sprintf("[%s] wipe=%t scan=%s",
							strings.Join(described, " "), wipe, scan),
						candidates: candidates,
						disks:      combo,
						wipe:       wipe,
						scan:       scan,
					})
				}
			}
		}
	}

	stub := func(m machine) func() {
		var partitioned, labelled []string
		for i, s := range m.disks {
			if s != diskEmpty {
				partitioned = append(partitioned, m.candidates[i])
			}
			if s == diskLabelled {
				labelled = append(labelled, m.candidates[i])
			}
		}
		return stubDisks(m.candidates, partitioned, labelled, m.wipe)
	}

	It("enumerates every machine this code can see", func() {
		// 3 disk states over 1, 2 and 3 disks, times two wipe settings,
		// times the three scan states that reach selectTargetDisk.
		Expect(machines).To(HaveLen((3 + 9 + 27) * 2 * 3))
	})

	It("never moves a both-missing boot off the disk pre-#4804 master picked", func() {
		// This is the data-loss case in the review. An encrypted post-#4403
		// install shows only COS_OEM_LUKS and COS_PERSISTENT_LUKS, so the
		// whole-machine scan reports both-missing while the disk itself
		// answers "kairos-labelled", the exact shape of the kairos-labelled
		// disks in this enumeration.
		var checked int
		for _, m := range machines {
			if m.scan.partial() {
				continue
			}
			restore := stub(m)
			got, err := selectTargetDisk("", m.scan.partial())
			restore()
			Expect(err).ToNot(HaveOccurred(), m.name)
			restore = stub(m)
			want := baselineSelectTargetDisk()
			restore()
			Expect(got).To(Equal(want), m.name)
			checked++
		}
		Expect(checked).To(Equal((3 + 9 + 27) * 2))
	})

	It("differs from #4804 as merged only on both-missing boots", func() {
		var diverged, agreed int
		for _, m := range machines {
			restore := stub(m)
			got, err := selectTargetDisk("", m.scan.partial())
			restore()
			Expect(err).ToNot(HaveOccurred(), m.name)
			restore = stub(m)
			merged := mergedSelectTargetDisk()
			restore()
			if got == merged {
				agreed++
				continue
			}
			Expect(m.scan.partial()).To(BeFalse(),
				"diverged from #4804 on a partial boot, which it must reproduce: "+m.name)
			diverged++
		}
		// The narrowing has to actually bite somewhere, or the gate is dead.
		Expect(diverged).To(BeNumerically(">", 0))
		Expect(agreed).To(BeNumerically(">", 0))
	})

	It("only overrides the baseline onto a disk that already carries a label", func() {
		// Where the selection does move, it moves onto a disk that
		// diskHasKairosPartitionsFn recognizes, so the wipe guard's
		// exemption applies for the reason it documents: the missing
		// sibling is appended next to the one that is there.
		var overrides int
		for _, m := range machines {
			restore := stub(m)
			got, err := selectTargetDisk("", m.scan.partial())
			restore()
			Expect(err).ToNot(HaveOccurred(), m.name)
			restore = stub(m)
			want := baselineSelectTargetDisk()
			isLabelled := diskHasKairosPartitionsFn(got)
			restore()
			if got == want {
				continue
			}
			Expect(isLabelled).To(BeTrue(), m.name)
			Expect(m.scan.partial()).To(BeTrue(), m.name)
			overrides++
		}
		Expect(overrides).To(BeNumerically(">", 0))
	})

	It("resolves the #4804 reproducer onto the disk carrying COS_OEM", func() {
		// Two non-removable disks, the larger empty, the smaller carrying
		// COS_OEM. CandidateDisks is largest-first, so the empty disk is
		// /dev/sda and the labelled one /dev/sdb.
		restore := stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sdb"},
			[]string{"/dev/sdb"},
			false,
		)
		defer restore()
		Expect(selectTargetDisk("", scanState{oem: true}.partial())).To(Equal("/dev/sdb"))
		// Pre-#4804 master put COS_PERSISTENT on the wrong disk here, which
		// is the bug the pull request fixes.
		Expect(baselineSelectTargetDisk()).To(Equal("/dev/sda"))
	})

	It("leaves an encrypted second disk alone on a both-missing boot", func() {
		// Same two disks, but the smaller one holds an encrypted install.
		// KairosPartitionsPresent sees COS_OEM_LUKS and COS_PERSISTENT_LUKS
		// and reports both-missing; DiskHasKairosPartitions matches the LUKS
		// labels and says the disk is ours. #4804 as merged steered the boot
		// onto it. This pull request leaves it where master did.
		restore := stubDisks(
			[]string{"/dev/sda", "/dev/sdb"},
			[]string{"/dev/sdb"},
			[]string{"/dev/sdb"},
			false,
		)
		defer restore()
		Expect(selectTargetDisk("", scanState{}.partial())).To(Equal("/dev/sda"))
		Expect(mergedSelectTargetDisk()).To(Equal("/dev/sdb"))
	})

	It("keeps the enumeration's disk names unique and ordered", func() {
		for _, m := range machines {
			sorted := append([]string{}, m.candidates...)
			sort.Strings(sorted)
			Expect(sorted).To(Equal(m.candidates), m.name)
		}
	})
})
