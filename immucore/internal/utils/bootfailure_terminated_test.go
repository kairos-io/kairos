package utils_test

import (
	"github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RenderTerminatedMessage", func() {
	It("uses the standard boot-failure banner", func() {
		out := utils.RenderTerminatedMessage("terminated", constants.LogDir)
		Expect(out).To(ContainSubstring("KAIROS BOOT FAILED"))
		Expect(out).To(ContainSubstring("root filesystem was never mounted"))
	})

	It("names the signal that terminated the run", func() {
		out := utils.RenderTerminatedMessage("interrupt", constants.LogDir)
		Expect(out).To(ContainSubstring("interrupt"))
	})

	It("says the mounts are missing rather than the disk is broken", func() {
		out := utils.RenderTerminatedMessage("terminated", constants.LogDir)
		Expect(out).To(ContainSubstring("/etc overlay"))
		Expect(out).To(ContainSubstring("/usr/local"))
	})

	It("points at the unit ordering as the repeat case", func() {
		out := utils.RenderTerminatedMessage("terminated", constants.LogDir)
		Expect(out).To(ContainSubstring("Before=initrd-switch-root.target"))
	})

	It("points at the log directory it was given", func() {
		out := utils.RenderTerminatedMessage("terminated", "/some/log/dir")
		Expect(out).To(ContainSubstring("/some/log/dir"))
	})

	It("carries the conventional section titles", func() {
		out := utils.RenderTerminatedMessage("terminated", constants.LogDir)
		Expect(out).To(ContainSubstring(utils.SectionWhatWentWrong))
		Expect(out).To(ContainSubstring(utils.SectionHowToFix))
	})
})
