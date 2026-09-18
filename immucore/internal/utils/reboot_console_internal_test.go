package utils

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RebootOrWait console output", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	// Regression for kairos-io/kairos#4618: the failure message used to go to
	// the logger only, which systemd routes to /dev/console, and /dev/console
	// aliases just the last console= on the cmdline. A headless machine booted
	// with `console=ttyS0 console=tty1` saw nothing at all.
	Context("announceOnConsoles", func() {
		It("writes the message to every console, not only the last one", func() {
			serial := filepath.Join(dir, "ttyS0")
			video := filepath.Join(dir, "tty1")
			for _, p := range []string{serial, video} {
				Expect(os.WriteFile(p, nil, 0o600)).To(Succeed())
			}

			announceOnConsoles([]string{serial, video}, "Unlocking partitions failed - Halting boot")

			for _, p := range []string{serial, video} {
				b, err := os.ReadFile(p)
				Expect(err).ToNot(HaveOccurred())
				Expect(string(b)).To(ContainSubstring("Unlocking partitions failed - Halting boot"),
					"console %s got no message", p)
			}
		})

		It("still writes the reachable consoles when one cannot be opened", func() {
			video := filepath.Join(dir, "tty1")
			Expect(os.WriteFile(video, nil, 0o600)).To(Succeed())
			missing := filepath.Join(dir, "ttyS0-absent")

			announceOnConsoles([]string{missing, video}, "boom")

			b, err := os.ReadFile(video)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(b)).To(ContainSubstring("boom"))
			Expect(missing).ToNot(BeAnExistingFile())
		})

		It("terminates every line with CRLF", func() {
			p := filepath.Join(dir, "console")
			Expect(os.WriteFile(p, nil, 0o600)).To(Succeed())

			announceOnConsoles([]string{p}, "first\nsecond")

			b, err := os.ReadFile(p)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(b)).To(Equal("first\r\nsecond\r\n"))
		})
	})

	Context("failureLine", func() {
		It("names the action when there is no error", func() {
			Expect(failureLine("Secure boot is not enabled", nil, "Halting boot")).
				To(Equal("Secure boot is not enabled - Halting boot"))
		})

		It("includes the error, which is the only diagnostic a headless boot gets", func() {
			line := failureLine("Unlocking partitions failed", errors.New("no such key"), "Rebooting in 10 seconds")
			Expect(line).To(ContainSubstring("Unlocking partitions failed"))
			Expect(line).To(ContainSubstring("no such key"))
			Expect(line).To(ContainSubstring("Rebooting in 10 seconds"))
		})
	})
})
