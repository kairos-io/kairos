package utils_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("image checksum settings", func() {
	var dir string

	cmdline := func(c string) {
		f := filepath.Join(dir, "cmdline")
		Expect(os.WriteFile(f, []byte(c), 0o644)).To(Succeed())
		Expect(os.Setenv("HOST_PROC_CMDLINE", f)).To(Succeed())
	}
	policy := func(p imagesum.Policy) {
		Expect(imagesum.WritePolicy(vfsOS(), dir, p)).To(Succeed())
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		cmdline("root=LABEL=COS_ACTIVE rd.immucore.debug")
	})
	AfterEach(func() { os.Unsetenv("HOST_PROC_CMDLINE") })

	It("defaults to not checking and not requiring", func() {
		s := utils.ResolveVerifySettings(dir)
		Expect(s.Action).To(Equal(imagesum.None))
		Expect(s.Require).To(BeFalse())
		Expect(s.ActionSource).To(Equal(utils.SourceDefault))
	})

	It("reads the policy file the agent wrote", func() {
		policy(imagesum.Policy{Verify: imagesum.Fail, Require: true})
		s := utils.ResolveVerifySettings(dir)
		Expect(s.Action).To(Equal(imagesum.Fail))
		Expect(s.Require).To(BeTrue())
		Expect(s.ActionSource).To(Equal(utils.SourcePolicy))
	})

	It("lets the cmdline override the policy file", func() {
		policy(imagesum.Policy{Verify: imagesum.Fail, Require: true})
		cmdline("image-checksum.verify=none image-checksum.require=false")
		s := utils.ResolveVerifySettings(dir)
		Expect(s.Action).To(Equal(imagesum.None))
		Expect(s.Require).To(BeFalse())
		Expect(s.ActionSource).To(Equal(utils.SourceCmdline))
		Expect(s.RequireSource).To(Equal(utils.SourceCmdline))
	})

	DescribeTable("reads the action from the cmdline",
		func(line string, want imagesum.Action) {
			cmdline(line)
			Expect(utils.ResolveVerifySettings(dir).Action).To(Equal(want))
		},
		Entry("warn", "image-checksum.verify=warn", imagesum.Warn),
		Entry("fail", "ro image-checksum.verify=fail quiet", imagesum.Fail),
		Entry("last one wins", "image-checksum.verify=fail image-checksum.verify=warn", imagesum.Warn),
		Entry("an old looking spelling is not read", "rd.immucore.verify_image=fail", imagesum.None),
	)

	DescribeTable("reads require from the cmdline",
		func(line string, want bool) {
			cmdline(line)
			Expect(utils.ResolveVerifySettings(dir).Require).To(Equal(want))
		},
		Entry("true", "image-checksum.require=true", true),
		Entry("false", "image-checksum.require=false", false),
		Entry("absent", "quiet", false),
	)

	It("ignores an unknown action on the cmdline and falls back to the policy file", func() {
		policy(imagesum.Policy{Verify: imagesum.Warn})
		cmdline("image-checksum.verify=sometimes")
		s := utils.ResolveVerifySettings(dir)
		Expect(s.Action).To(Equal(imagesum.Warn))
		Expect(s.ActionSource).To(Equal(utils.SourcePolicy))
	})

	It("ignores a broken policy file", func() {
		Expect(os.WriteFile(filepath.Join(dir, imagesum.PolicyFile), []byte("verify: sometimes\n"), 0o644)).To(Succeed())
		Expect(utils.ResolveVerifySettings(dir).Action).To(Equal(imagesum.None))
	})

	Describe("the failure screen", func() {
		ansi := regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
		expected := strings.Repeat("a1", 32)
		actual := strings.Repeat("b2", 32)

		// the banner bars are 78 columns wide, the text must fit under them
		expectFits := func(out string) {
			for _, line := range strings.Split(out, "\n") {
				Expect(len(strings.TrimRight(line, " "))).To(BeNumerically("<=", 78), line)
			}
		}

		render := func(p utils.ImageChecksumProblem, s utils.VerifySettings) string {
			out := utils.RenderImageChecksumFailedMessage(p, s)
			// show what the operator sees, for the test log
			GinkgoWriter.Println("\n" + ansi.ReplaceAllString(out, ""))
			return ansi.ReplaceAllString(out, "")
		}

		It("names the image and both digests on lines that fit an 80 column console", func() {
			out := render(utils.ImageChecksumProblem{
				Image: "/run/initramfs/cos-state/cOS/active.img", Kind: "checksum mismatch: the image is not what was written",
				Expected: expected, Actual: actual,
			}, utils.VerifySettings{Action: imagesum.Fail, ActionSource: utils.SourceCmdline})
			Expect(out).To(ContainSubstring("KAIROS BOOT FAILED"))
			Expect(out).To(ContainSubstring("OS image checksum check failed"))
			Expect(out).To(ContainSubstring("/run/initramfs/cos-state/cOS/active.img"))
			Expect(out).To(ContainSubstring("Expected sha256 (stored in active.img.sha256):\n    " + expected))
			Expect(out).To(ContainSubstring("Actual sha256 (computed from the image just now):\n    " + actual))
			Expect(out).To(ContainSubstring("Action: fail (from kernel cmdline)"))
			Expect(out).To(ContainSubstring("image-checksum.verify=warn"))
			expectFits(out)
		})

		It("says so when a digest is not available and when require is on", func() {
			out := render(utils.ImageChecksumProblem{
				Image: "/run/initramfs/cos-state/cOS/passive.img", Kind: "no checksum recorded for this image",
			}, utils.VerifySettings{Action: imagesum.Fail, Require: true, ActionSource: utils.SourcePolicy, RequireSource: utils.SourcePolicy})
			Expect(out).To(ContainSubstring("(not available)"))
			Expect(out).To(ContainSubstring("require is on"))
			Expect(out).To(ContainSubstring(utils.SourcePolicy))
			expectFits(out)
		})
	})
})
