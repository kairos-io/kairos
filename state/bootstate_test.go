package state

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v4/vfst"
)

var _ = DescribeTable("getNonUKIBootState",
	func(cmdline string, want Boot) { Expect(getNonUKIBootState(cmdline)).To(Equal(want)) },
	Entry("active", "root=LABEL=COS_ACTIVE ro", Active),
	Entry("passive", "root=LABEL=COS_PASSIVE ro", Passive),
	Entry("recovery", "root=LABEL=COS_RECOVERY ro", Recovery),
	Entry("recovery via recovery-mode", "root=/dev/sda2 recovery-mode ro", Recovery),
	// The statereset boot carries kairos.reset on a COS_RECOVERY command line;
	// kairos.reset must win => AutoReset, not Recovery.
	Entry("autoreset (statereset)", "root=LABEL=COS_RECOVERY vga=795 kairos.reset", AutoReset),
	Entry("livecd", "root=live:LABEL=COS_LIVE ro", LiveCD),
	Entry("livecd via netboot", "root=/dev/nfs netboot ip=dhcp", LiveCD),
	// A cmdline with no known marker must NOT be reported as a false "active".
	Entry("no marker => Unknown", "root=/dev/vda1 ro quiet", Unknown),
)

var _ = DescribeTable("DetectInRAM",
	func(cmdline string, want bool) { Expect(DetectInRAM(cmdline)).To(Equal(want)) },
	Entry("absent", "root=LABEL=COS_ACTIVE ro quiet", false),
	Entry("bare toggle", "kairos.ram", true),
	Entry("bare toggle alongside livecd", "root=live:LABEL=COS_LIVE ro kairos.ram rd.live.ram=1", true),
	Entry("bare toggle alongside recovery", "root=LABEL=COS_RECOVERY kairos.ram", true),
	Entry("bare toggle with ignored value", "kairos.ram=yes", true),
	Entry("child kairos.ram.auto_create_partitions", "root=LABEL=COS_ACTIVE kairos.ram.auto_create_partitions", true),
	Entry("child kairos.ram.oem=64", "root=LABEL=COS_ACTIVE kairos.ram.oem=64", true),
	Entry("child kairos.ram.wipe", "root=LABEL=COS_ACTIVE kairos.ram.wipe", true),
	Entry("substring without namespace boundary must NOT match", "root=/dev/sda1 kairos.ramnottreally=1", false),
	Entry("empty cmdline", "", false),
)

var _ = Describe("DetectInRAMWithVFS", func() {
	withCmdline := func(cmdline string) *vfst.TestFS {
		fs, _, err := vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": cmdline})
		Expect(err).ToNot(HaveOccurred())
		return fs
	}

	It("returns true when kairos.ram is present", func() {
		v, err := DetectInRAMWithVFS(withCmdline("root=live:LABEL=COS_LIVE ro kairos.ram"))
		Expect(err).ToNot(HaveOccurred())
		Expect(v).To(BeTrue())
	})

	It("returns false when kairos.ram is absent", func() {
		v, err := DetectInRAMWithVFS(withCmdline("root=LABEL=COS_ACTIVE ro"))
		Expect(err).ToNot(HaveOccurred())
		Expect(v).To(BeFalse())
	})

	It("returns an error when /proc/cmdline is unreadable", func() {
		fs, _, err := vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		v, err := DetectInRAMWithVFS(fs)
		Expect(err).To(HaveOccurred())
		Expect(v).To(BeFalse())
	})
})

var _ = Describe("DetectBootWithVFS", func() {
	withCmdline := func(cmdline string) *vfst.TestFS {
		fs, _, err := vfst.NewTestFS(map[string]interface{}{"/proc/cmdline": cmdline})
		Expect(err).ToNot(HaveOccurred())
		return fs
	}

	It("recognises the statereset (autoreset) boot", func() {
		b, err := DetectBootWithVFS(withCmdline("root=LABEL=COS_RECOVERY kairos.reset"))
		Expect(err).ToNot(HaveOccurred())
		Expect(b).To(Equal(AutoReset))
	})

	It("recognises an active boot", func() {
		b, err := DetectBootWithVFS(withCmdline("root=LABEL=COS_ACTIVE ro"))
		Expect(err).ToNot(HaveOccurred())
		Expect(b).To(Equal(Active))
	})

	It("classifies an in-RAM boot as Active even from a live medium", func() {
		b, err := DetectBootWithVFS(withCmdline("root=live:LABEL=COS_LIVE ro kairos.ram rd.live.ram=1"))
		Expect(err).ToNot(HaveOccurred())
		Expect(b).To(Equal(Active))
	})

	It("returns an error (and Unknown) when /proc/cmdline is unreadable", func() {
		fs, _, err := vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ToNot(HaveOccurred())
		b, err := DetectBootWithVFS(fs)
		Expect(err).To(HaveOccurred())
		Expect(b).To(Equal(Unknown))
	})
})
