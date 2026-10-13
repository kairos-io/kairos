package state

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InstallIntentSentinels", func() {
	const (
		plain       = InstallModeSentinel
		interactive = InstallModeInteractiveSentinel
	)

	// grub is a GRUB live ISO: not a UKI, and immucore reports LiveCD.
	grub := func(cmdline string) []string {
		return InstallIntentSentinels(cmdline, false, true)
	}

	Describe("the GRUB live ISO, where the entry states the intent", func() {
		It("asks for the plain installer on install-mode", func() {
			Expect(grub("BOOT_IMAGE=/boot/kernel install-mode")).To(Equal([]string{plain}))
		})

		It("asks for the interactive installer on install-mode-interactive", func() {
			Expect(grub("BOOT_IMAGE=/boot/kernel install-mode-interactive rd.debug")).
				To(Equal([]string{interactive}))
		})

		It("asks for the interactive installer on the legacy interactive-install", func() {
			Expect(grub("BOOT_IMAGE=/boot/kernel interactive-install")).To(Equal([]string{interactive}))
		})

		// install-mode is a prefix of install-mode-interactive. Matching it as
		// a substring would hand the same boot to both installers, and only
		// the order of the stages in 52_installer.yaml would decide which one
		// ends up owning tty1.
		It("does not read install-mode out of install-mode-interactive", func() {
			Expect(grub("BOOT_IMAGE=/boot/kernel install-mode-interactive")).
				NotTo(ContainElement(plain))
		})

		// A keyword embedded in another token is not that keyword. A substring
		// match would fire on a cmdline that merely names one, such as a
		// kernel argument whose value happens to end in the word.
		It("matches whole tokens only", func() {
			for _, cmdline := range []string{
				"BOOT_IMAGE=/boot/kernel no-install-mode",
				"BOOT_IMAGE=/boot/kernel foo=install-mode",
				"BOOT_IMAGE=/boot/kernel install-modes",
				"BOOT_IMAGE=/boot/kernel skip-interactive-install",
			} {
				Expect(InstallIntentSentinels(cmdline, false, true)).To(BeEmpty(), "cmdline %q", cmdline)
			}
		})

		// Both stages ran when the cloud-config grepped for both keywords, and
		// the interactive one comes later in the file and takes tty1 back.
		It("asks for both when the entry names both keywords", func() {
			Expect(grub("BOOT_IMAGE=/boot/kernel install-mode install-mode-interactive")).
				To(Equal([]string{plain, interactive}))
		})

		It("asks for nothing on a live boot that names neither", func() {
			Expect(grub("BOOT_IMAGE=/boot/kernel nodepair.enable")).To(BeEmpty())
		})
	})

	Describe("Trusted Boot, where the UKI cannot state anything", func() {
		It("asks for the interactive installer whatever the cmdline says", func() {
			for _, cmdline := range []string{
				"BOOT_IMAGE=/boot/kernel install-mode",
				"BOOT_IMAGE=/boot/kernel install-mode-interactive",
				"BOOT_IMAGE=/boot/kernel",
			} {
				Expect(InstallIntentSentinels(cmdline, true, true)).
					To(Equal([]string{interactive}), "cmdline %q", cmdline)
			}
		})

		// An installed Trusted Boot system keeps install-mode on its cmdline
		// for the life of the machine, because the installer copies
		// norole.efi byte for byte into every role. It is not install media,
		// so it must install nothing. This is kairos-io/kairos#5000.
		It("asks for nothing once the system is installed", func() {
			Expect(InstallIntentSentinels("BOOT_IMAGE=/boot/kernel install-mode rd.immucore.uki", false, false)).
				To(BeEmpty())
		})
	})

	// The in-RAM workflow boots from removable or network media on purpose and
	// must behave like an installed Active system. kairos-sdk forces
	// BootState=Active there, so liveCD is false and no installer starts.
	It("asks for nothing outside live media, whatever the cmdline says", func() {
		for _, cmdline := range []string{
			"BOOT_IMAGE=/boot/kernel install-mode",
			"BOOT_IMAGE=/boot/kernel install-mode-interactive",
			"BOOT_IMAGE=/boot/kernel interactive-install",
		} {
			Expect(InstallIntentSentinels(cmdline, false, false)).To(BeEmpty(), "cmdline %q", cmdline)
		}
	})
})
