package utils_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/immucore/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("failure summary", func() {
	var fakeCmdline string

	BeforeEach(func() {
		f, err := os.CreateTemp("", "fake-cmdline")
		Expect(err).ToNot(HaveOccurred())
		_, err = f.WriteString("rd.immucore.uki root=LABEL=COS_ACTIVE rd.immucore.debug\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Close()).ToNot(HaveOccurred())
		fakeCmdline = f.Name()
		err = os.Setenv("HOST_PROC_CMDLINE", fakeCmdline)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		_ = os.Unsetenv("HOST_PROC_CMDLINE")
		_ = os.Remove(fakeCmdline)
	})

	Describe("RenderFailureSummary", func() {
		It("includes a bold header", func() {
			out := utils.RenderFailureSummary("something failed", constants.LogDir)
			Expect(out).To(ContainSubstring("IMMUCORE BOOT FAILED"))
		})

		It("includes the reason passed in", func() {
			out := utils.RenderFailureSummary("failed to mount sysroot", constants.LogDir)
			Expect(out).To(ContainSubstring("failed to mount sysroot"))
		})

		It("includes the kernel cmdline content", func() {
			out := utils.RenderFailureSummary("boom", constants.LogDir)
			Expect(out).To(ContainSubstring("rd.immucore.uki root=LABEL=COS_ACTIVE rd.immucore.debug"))
		})

		It("includes the log directory location passed in", func() {
			out := utils.RenderFailureSummary("boom", "/custom/log/dir")
			Expect(out).To(ContainSubstring("/custom/log/dir"))
		})

		It("includes the inspect/retry hint", func() {
			out := utils.RenderFailureSummary("boom", constants.LogDir)
			Expect(out).To(ContainSubstring("Inspect logs above, then exit this shell to retry or reboot."))
		})

		It("handles an empty reason gracefully", func() {
			out := utils.RenderFailureSummary("", constants.LogDir)
			Expect(out).To(ContainSubstring("IMMUCORE BOOT FAILED"))
		})
	})

	Describe("WriteFailureSummary", func() {
		It("writes the summary to a file under the given dir", func() {
			tmpDir, err := os.MkdirTemp("", "logdir")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			summary := utils.RenderFailureSummary("disk on fire", tmpDir)
			path, err := utils.WriteFailureSummary(tmpDir, summary)
			Expect(err).ToNot(HaveOccurred())
			Expect(path).To(HavePrefix(tmpDir))

			content, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("disk on fire"))
		})

		It("persists the summary root-only (cmdline may carry secrets)", func() {
			tmpDir, err := os.MkdirTemp("", "logdir-perms")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			path, err := utils.WriteFailureSummary(tmpDir, "secret token=abc")
			Expect(err).ToNot(HaveOccurred())

			info, err := os.Stat(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		It("creates the dir if it does not exist", func() {
			base, err := os.MkdirTemp("", "logbase")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(base)
			target := filepath.Join(base, "nested", "immucore")

			path, err := utils.WriteFailureSummary(target, "some summary")
			Expect(err).ToNot(HaveOccurred())
			content, err := os.ReadFile(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("some summary"))
		})
	})
})
