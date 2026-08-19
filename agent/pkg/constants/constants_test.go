package constants_test

import (
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetUserConfigDirs", func() {
	It("no longer reads the deprecated /etc/elemental directory", func() {
		Expect(constants.GetUserConfigDirs()).NotTo(ContainElement("/etc/elemental"))
	})

	It("still reads the supported config directories", func() {
		Expect(constants.GetUserConfigDirs()).To(ContainElements(
			"/run/initramfs/live",
			"/etc/kairos",
			"/usr/local/cloud-config",
			"/oem",
		))
	})
})

var _ = Describe("Replace title", func() {
	DescribeTable("Replacing the tile",
		func(role, oldTitle, newTitle string) {
			Expect(constants.BootTitleForRole(role, oldTitle)).To(Equal(newTitle))
		},
		Entry("When seeting to active with a default title", "active", "My awesome OS", "My awesome OS"),
		Entry("When setting to active with a fallback title", "active", "My awesome OS (fallback)", "My awesome OS"),
		Entry("When setting to active with a recovery title", "active", "My awesome OS recovery", "My awesome OS"),
		Entry("When setting to passive with a default title", "passive", "My awesome OS", "My awesome OS (fallback)"),
		Entry("When setting to passive with a fallback title", "passive", "My awesome OS (fallback)", "My awesome OS (fallback)"),
		Entry("When setting to passive with a recovery title", "passive", "My awesome OS recovery", "My awesome OS (fallback)"),
		Entry("When setting to recovery with a default title", "recovery", "My awesome OS", "My awesome OS recovery"),
		Entry("When setting to recovery with a fallback title", "recovery", "My awesome OS (fallback)", "My awesome OS recovery"),
		Entry("When setting to recovery with a recovery title", "recovery", "My awesome OS recovery", "My awesome OS recovery"),
	)
})

var _ = Describe("Constants helpers", func() {
	It("returns the expected fallback EFI binary per architecture", func() {
		Expect(constants.GetFallBackEfi(constants.ArchArm64)).To(Equal("bootaa64.efi"))
		Expect(constants.GetFallBackEfi(constants.ArchRiscv64)).To(Equal("BOOTRISCV64.EFI"))
		Expect(constants.GetFallBackEfi(constants.ArchAmd64)).To(Equal("bootx64.efi"))
	})

	It("returns the default UKI menu and skip entries", func() {
		Expect(constants.UkiDefaultMenuEntries()).To(Equal([]string{"cos", "fallback", "recovery", "statereset"}))
		Expect(constants.UkiDefaultSkipEntries()).To(Equal([]string{"interactive-install", "install-mode-interactive"}))
	})

	It("returns the default cloud-init and yip config paths", func() {
		Expect(constants.GetCloudInitPaths()).To(Equal([]string{"/system/oem", "/oem/", "/usr/local/cloud-config/"}))
		Expect(constants.GetYipConfigDirs()).To(ContainElements("/system/oem"))
		Expect(constants.GetYipConfigDirs()).To(ContainElements(
			"/run/initramfs/live",
			"/etc/kairos",
			"/usr/local/cloud-config",
			"/oem",
		))
	})

	It("returns the default squashfs and grub assets", func() {
		Expect(constants.GetDefaultSquashfsOptions()).To(Equal([]string{"-b", "1024k"}))
		Expect(constants.GetDefaultSquashfsCompressionOptions()).To(Equal([]string{"-comp", "gzip"}))
		Expect(constants.GetGrubFonts()).To(ContainElements("ascii.pf2", "unicode.pf2"))
		Expect(constants.GetGrubModules()).To(ContainElements("loopback.mod", "squash4.mod"))
	})

	It("strips known boot suffixes from titles", func() {
		Expect(constants.BaseBootTitle("My OS recovery")).To(Equal("My OS"))
		Expect(constants.BaseBootTitle("My OS (fallback)")).To(Equal("My OS"))
		Expect(constants.BaseBootTitle("My OS state reset (auto)")).To(Equal("My OS"))
		Expect(constants.BaseBootTitle("My OS")).To(Equal("My OS"))
	})

	It("returns an error for invalid boot roles", func() {
		_, err := constants.BootTitleForRole("invalid", "My OS")
		Expect(err).To(HaveOccurred())
	})
})
