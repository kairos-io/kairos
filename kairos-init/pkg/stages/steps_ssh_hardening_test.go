package stages_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
)

var _ = Describe("GetSshHardeningStage", func() {
	var log logger.KairosLogger

	BeforeEach(func() {
		log = logger.NewKairosLogger("test", "error", true)
	})

	Context("with default config", func() {
		var result []schema.Stage

		BeforeEach(func() {
			result = stages.GetSshHardeningStage(values.System{}, log)
		})

		It("returns at least one stage", func() {
			Expect(result).ToNot(BeEmpty())
		})

		It("installs the hardening drop-in as a single 0644 root-owned file", func() {
			install := result[0]
			Expect(install.Files).To(HaveLen(1))

			f := install.Files[0]
			Expect(f.Path).To(Equal(bundled.SshdHardeningPath))
			Expect(f.Permissions).To(Equal(uint32(0o644)))
			Expect(f.Owner).To(BeZero())
			Expect(f.Group).To(BeZero())
		})

		It("disables root login and X11 forwarding in the drop-in", func() {
			content := result[0].Files[0].Content
			Expect(content).To(ContainSubstring("PermitRootLogin no"))
			Expect(content).To(ContainSubstring("X11Forwarding no"))
		})

		It("leaves PasswordAuthentication untouched", func() {
			// See bundled.SshdHardeningConfig's docstring: auth-mode
			// controls are deliberately deferred so first-boot password
			// login keeps working.
			Expect(result[0].Files[0].Content).ToNot(ContainSubstring("PasswordAuthentication"))
		})

		It("filters weak Diffie-Hellman moduli via awk", func() {
			var moduliStage *schema.Stage
			for i := range result {
				if result[i].Name == "Filter weak Diffie-Hellman moduli" {
					moduliStage = &result[i]
					break
				}
			}
			Expect(moduliStage).ToNot(BeNil(), "expected a moduli-filtering stage")

			cmds := strings.Join(moduliStage.Commands, "\n")
			Expect(cmds).To(ContainSubstring("awk"))
			Expect(cmds).To(SatisfyAny(
				ContainSubstring("2047"),
				ContainSubstring("2048"),
			))
		})
	})

	Context("when the skip step is configured", func() {
		var previous []string

		BeforeEach(func() {
			previous = config.DefaultConfig.SkipSteps
			config.DefaultConfig.SkipSteps = []string{values.SshHardeningStep}
		})

		AfterEach(func() {
			config.DefaultConfig.SkipSteps = previous
		})

		It("returns no stages", func() {
			Expect(stages.GetSshHardeningStage(values.System{}, log)).To(BeEmpty())
		})
	})
})
