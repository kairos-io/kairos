package ops

import (
	"os"
	"path/filepath"

	agentConstants "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testGrubCfg = "menuentry cos {}\n"

func writeTestFile(path, content string) {
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}

var _ = Describe("Raw disk state partition", Label("raw"), func() {
	var (
		rootfs string
		state  string
		r      *RawImage
	)

	BeforeEach(func() {
		rootfs = GinkgoT().TempDir()
		state = GinkgoT().TempDir()
		writeTestFile(filepath.Join(rootfs, "etc/kairos-release"), "KAIROS_ARCH=amd64\n")
		writeTestFile(filepath.Join(rootfs, agentConstants.GrubConf), testGrubCfg)
		for _, m := range agentConstants.GetGrubModules() {
			writeTestFile(filepath.Join(rootfs, "usr/lib/grub/arm64-efi", m), "arm64")
			writeTestFile(filepath.Join(rootfs, "usr/lib/grub/x86_64-efi", m), "x86_64")
		}
		writeTestFile(filepath.Join(rootfs, "usr/share/grub/unicode.pf2"), "font")
		r = NewEFIRawImage(RawImageParams{Source: rootfs, BootActive: true})
	})

	It("writes grub config, arch modules and boot assessment files", func() {
		Expect(r.writeStateGrubFiles(state)).To(Succeed())

		cfg, err := os.ReadFile(filepath.Join(state, stateGrubDir, stateGrubCfgFile))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(cfg)).To(Equal(testGrubCfg))

		for _, m := range agentConstants.GetGrubModules() {
			content, err := os.ReadFile(filepath.Join(state, stateGrubDir, "x86_64-efi", m))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(Equal("x86_64"))
		}
		Expect(filepath.Join(state, stateGrubDir, "x86_64-efi", grubFontsDir, "unicode.pf2")).To(BeARegularFile())
		Expect(filepath.Join(state, stateGrubDir, "arm64-efi")).ToNot(BeADirectory())

		Expect(filepath.Join(state, stateGrubCustomFile)).To(BeARegularFile())
		Expect(filepath.Join(state, stateBootAssessmentFile)).To(BeARegularFile())
		Expect(filepath.Join(state, stateGrubMenuFile)).ToNot(BeAnExistingFile())
	})

	It("copies grub branding when the rootfs ships it", func() {
		writeTestFile(filepath.Join(rootfs, rootfsBrandingGrubMenu), "branding")
		Expect(r.writeStateGrubFiles(state)).To(Succeed())
		content, err := os.ReadFile(filepath.Join(state, stateGrubMenuFile))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("branding"))
	})

	It("fails when a grub module for the rootfs arch is missing", func() {
		Expect(os.RemoveAll(filepath.Join(rootfs, "usr/lib/grub/x86_64-efi"))).To(Succeed())
		err := r.writeStateGrubFiles(state)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("x86_64"))
	})

	It("requires the system image size to be calculated first", func() {
		_, err := r.createStatePartitionImage()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("system image size"))
	})

	It("sizes the system image from the rootfs without building recovery", func() {
		size, err := r.systemImageSize()
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(BeNumerically(">=", 200))

		r.RecoveryImageSize = 5000
		size, err = r.systemImageSize()
		Expect(err).ToNot(HaveOccurred())
		Expect(size).To(Equal(uint(5000)))
	})

	DescribeTable("sizes the state partition",
		func(systemImage uint, slots int, configured int64, expected uint) {
			Expect(statePartitionSize(systemImage, slots, configured)).To(Equal(expected))
		},
		Entry("default slots", uint(1000), 0, int64(0), uint(3445)),
		Entry("active only", uint(1000), 1, int64(0), uint(1223)),
		Entry("room for one upgrade", uint(1000), 2, int64(0), uint(2334)),
		Entry("full A/B", uint(1000), 3, int64(0), uint(3445)),
		// Regression: 2319M active.img did not fit a 2419M ext4 STATE
		Entry("kubeadm image, active only", uint(2319), 1, int64(0), uint(2688)),
		Entry("configured size wins over slots", uint(1000), 1, int64(5000), uint(5000)),
	)

	DescribeTable("ships passive.img when STATE has room for it",
		func(slots int, expected bool) {
			Expect(statePassiveEnabled(slots)).To(Equal(expected))
		},
		Entry("default slots", 0, true),
		Entry("active only", 1, false),
		Entry("room for one upgrade", 2, false),
		Entry("full A/B", 3, true),
	)

	DescribeTable("orders the partitions after the boot partition",
		func(recovery string, expected []string) {
			var names []string
			for _, p := range diskParts("oem.img", recovery, "state.img") {
				names = append(names, p.name)
			}
			Expect(names).To(Equal(expected))
		},
		Entry("with recovery", "recovery.img", []string{sdkConstants.OEMPartName, agentConstants.RecoveryImgName, sdkConstants.StatePartName}),
		Entry("without recovery", "", []string{sdkConstants.OEMPartName, sdkConstants.StatePartName}),
	)
})
