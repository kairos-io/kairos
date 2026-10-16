package constants_test

import (
	"strings"

	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Constants functions", func() {
	Describe("Static slice getters", func() {
		It("UkiDefaultMenuEntries returns the expected entries", func() {
			Expect(cnst.UkiDefaultMenuEntries()).To(Equal([]string{"cos", "fallback", "recovery", "statereset"}))
		})

		It("UkiDefaultSkipEntries returns the expected entries", func() {
			Expect(cnst.UkiDefaultSkipEntries()).To(Equal([]string{"interactive-install", "install-mode-interactive"}))
		})

		It("GetCloudInitPaths returns the expected paths", func() {
			Expect(cnst.GetCloudInitPaths()).To(Equal([]string{"/system/oem", "/oem/", "/usr/local/cloud-config/"}))
		})

		It("GetDefaultSquashfsOptions returns the expected options", func() {
			Expect(cnst.GetDefaultSquashfsOptions()).To(Equal([]string{"-b", "1024k"}))
		})

		It("GetDefaultSquashfsCompressionOptions returns the expected options", func() {
			Expect(cnst.GetDefaultSquashfsCompressionOptions()).To(Equal([]string{"-comp", "gzip"}))
		})

		It("GetGrubFonts returns the expected font files", func() {
			Expect(cnst.GetGrubFonts()).To(Equal([]string{"ascii.pf2", "euro.pf2", "unicode.pf2"}))
		})

		It("GetGrubModules returns the expected module files", func() {
			Expect(cnst.GetGrubModules()).To(Equal([]string{"loopback.mod", "squash4.mod", "xzio.mod", "gzio.mod", "regexp.mod"}))
		})

		It("GetUserConfigDirs returns the expected dirs in order", func() {
			Expect(cnst.GetUserConfigDirs()).To(Equal([]string{
				"/run/initramfs/live",
				"/etc/kairos",
				"/usr/local/cloud-config",
				"/oem",
			}))
		})

		It("GetYipConfigDirs appends /system/oem to user config dirs", func() {
			expected := append(cnst.GetUserConfigDirs(), "/system/oem")
			Expect(cnst.GetYipConfigDirs()).To(Equal(expected))
			Expect(cnst.GetYipConfigDirs()).To(ContainElement("/system/oem"))
		})
	})

	DescribeTable("GetFallBackEfi",
		func(arch, expected string) {
			Expect(cnst.GetFallBackEfi(arch)).To(Equal(expected))
		},
		Entry("amd64", cnst.ArchAmd64, "bootx64.efi"),
		Entry("arm64", cnst.ArchArm64, "bootaa64.efi"),
		Entry("riscv64", cnst.ArchRiscv64, "BOOTRISCV64.EFI"),
		Entry("unknown arch defaults to bootx64.efi", "ppc64le", "bootx64.efi"),
		Entry("empty arch defaults to bootx64.efi", "", "bootx64.efi"),
	)

	// These are the names AuroraBoot's build-uki writes into EFI/BOOT on the ESP,
	// so they are upper case for every architecture. They are deliberately not the
	// names GetFallBackEfi returns, which is the GRUB path's shim, written by this
	// repository under EFI/boot.
	DescribeTable("GetSystemdBootFallBackEfi",
		func(arch, expected string) {
			Expect(cnst.GetSystemdBootFallBackEfi(arch)).To(Equal(expected))
		},
		Entry("amd64", cnst.ArchAmd64, "BOOTX64.EFI"),
		Entry("arm64", cnst.ArchArm64, "BOOTAA64.EFI"),
		Entry("riscv64", cnst.ArchRiscv64, "BOOTRISCV64.EFI"),
		Entry("unknown arch defaults to BOOTX64.EFI", "ppc64le", "BOOTX64.EFI"),
		Entry("empty arch defaults to BOOTX64.EFI", "", "BOOTX64.EFI"),
	)

	It("GetSystemdBootFallBackEfi names a different file from GetFallBackEfi on riscv64 only", func() {
		// On amd64 and arm64 the two differ only in case, and an ESP is vfat, so
		// they reach the same file. On riscv64 the name itself differs, which is
		// why a reader that does not know riscv64 finds nothing there.
		for _, arch := range []string{cnst.ArchAmd64, cnst.ArchArm64} {
			Expect(strings.EqualFold(cnst.GetFallBackEfi(arch), cnst.GetSystemdBootFallBackEfi(arch))).
				To(BeTrue(), "arch %s", arch)
		}
		Expect(strings.EqualFold(cnst.GetFallBackEfi(cnst.ArchRiscv64), cnst.GetSystemdBootFallBackEfi(cnst.ArchAmd64))).
			To(BeFalse())
	})

	DescribeTable("BaseBootTitle strips known suffixes",
		func(title, expected string) {
			Expect(cnst.BaseBootTitle(title)).To(Equal(expected))
		},
		Entry("recovery suffix", "Kairos"+cnst.RecoveryBootSuffix, "Kairos"),
		Entry("passive/fallback suffix", "Kairos"+cnst.PassiveBootSuffix, "Kairos"),
		Entry("state reset suffix", "Kairos"+cnst.StateResetBootSuffix, "Kairos"),
		Entry("no suffix returns as-is", "Kairos", "Kairos"),
		Entry("empty string", "", ""),
		Entry("only strips trailing suffix once", "Kairos"+cnst.RecoveryBootSuffix+cnst.RecoveryBootSuffix, "Kairos"+cnst.RecoveryBootSuffix),
	)

	Describe("BootTitleForRole", func() {
		DescribeTable("known roles",
			func(role, title, expected string) {
				got, err := cnst.BootTitleForRole(role, title)
				Expect(err).ToNot(HaveOccurred())
				Expect(got).To(Equal(expected))
			},
			Entry("active role uses base title", cnst.ActiveImgName, "Kairos", "Kairos"),
			Entry("passive role appends fallback suffix", cnst.PassiveImgName, "Kairos", "Kairos"+cnst.PassiveBootSuffix),
			Entry("recovery role appends recovery suffix", cnst.RecoveryImgName, "Kairos", "Kairos"+cnst.RecoveryBootSuffix),
			Entry("statereset role appends state reset suffix", cnst.StateResetImgName, "Kairos", "Kairos"+cnst.StateResetBootSuffix),
			Entry("active role strips existing suffix first", cnst.ActiveImgName, "Kairos"+cnst.RecoveryBootSuffix, "Kairos"),
			Entry("passive role normalizes existing suffix", cnst.PassiveImgName, "Kairos"+cnst.RecoveryBootSuffix, "Kairos"+cnst.PassiveBootSuffix),
		)

		It("returns an error for an unknown role", func() {
			got, err := cnst.BootTitleForRole("bogus", "Kairos")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("invalid role"))
			Expect(got).To(Equal(""))
		})
	})
})
