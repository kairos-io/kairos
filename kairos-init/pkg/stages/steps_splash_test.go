package stages_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/stages"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/schema"
)

// hasFile reports whether any of the stages writes path.
func hasFile(result []schema.Stage, path string) bool {
	for _, st := range result {
		for _, f := range st.Files {
			if f.Path == path {
				return true
			}
		}
	}
	return false
}

var _ = Describe("the boot splash wiring", func() {
	log := logger.NewKairosLogger("test", "info", true)

	Describe("the initramfs half", func() {
		var result []schema.Stage

		BeforeEach(func() {
			var err error
			result, err = stages.GetKairosInitramfsFilesStage(values.System{}, log)
			Expect(err).ToNot(HaveOccurred())
		})

		It("writes the whole dracut module", func() {
			for _, p := range []string{
				bundled.DracutSplashPath,
				bundled.DracutSplashModuleSetupPath,
				bundled.DracutSplashServicePath,
				bundled.DracutSplashImmucoreQuietPath,
			} {
				Expect(fileByPath(result, p).Content).ToNot(BeEmpty(), p)
			}
		})

		// dracut sources module-setup.sh as a program, not as data. A 0644
		// module-setup.sh is silently skipped and the splash never lands in
		// the initramfs, with nothing in the build log to say so.
		It("makes the module setup script executable", func() {
			Expect(fileByPath(result, bundled.DracutSplashModuleSetupPath).Permissions).
				To(Equal(uint32(0o755)))
		})

		It("keeps the units and the drop-in as data", func() {
			Expect(fileByPath(result, bundled.DracutSplashServicePath).Permissions).
				To(Equal(uint32(0o644)))
			Expect(fileByPath(result, bundled.DracutSplashImmucoreQuietPath).Permissions).
				To(Equal(uint32(0o644)))
		})

		// A trusted-boot image has no dracut-built initrd at all, so shipping
		// a dracut module for it would be writing files nothing ever reads.
		It("ships nothing for trusted boot", func() {
			previous := config.DefaultConfig.TrustedBoot
			config.DefaultConfig.TrustedBoot = true
			defer func() { config.DefaultConfig.TrustedBoot = previous }()

			trusted, err := stages.GetKairosInitramfsFilesStage(values.System{}, log)
			Expect(err).ToNot(HaveOccurred())
			Expect(hasFile(trusted, bundled.DracutSplashPath)).To(BeFalse())
			Expect(hasFile(trusted, bundled.DracutSplashModuleSetupPath)).To(BeFalse())
		})
	})

	Describe("the booted-system half", func() {
		var result []schema.Stage

		BeforeEach(func() {
			result = stages.GetServicesStage(values.System{}, log)
		})

		It("writes the unit with the duration substituted", func() {
			unit := fileByPath(result, bundled.SplashServicePath)
			Expect(unit.Content).To(Equal(fmt.Sprintf(bundled.SplashService, bundled.SplashDuration)))
			Expect(unit.Content).ToNot(ContainSubstring("%!s"))
			Expect(unit.Permissions).To(Equal(uint32(0o644)))
		})

		// Without the enable there is no multi-user.target.wants link and the
		// unit is inert: the animation would stop at switch-root and the rest
		// of the boot would scroll systemd output over a bare console.
		It("enables the unit", func() {
			var enabled []string
			for _, st := range result {
				enabled = append(enabled, st.Systemctl.Enable...)
			}
			Expect(enabled).To(ContainElement("kairos-splash"))
		})

		// The unit file has to exist before systemctl enable looks for it.
		// yip runs EnsureFiles before Systemctl within one stage, so the pair
		// must stay in the same stage rather than becoming two.
		It("writes and enables it in the same stage", func() {
			for _, st := range result {
				for _, f := range st.Files {
					if f.Path == bundled.SplashServicePath {
						Expect(st.Systemctl.Enable).To(ContainElement("kairos-splash"))
						return
					}
				}
			}
			Fail("no stage writes " + bundled.SplashServicePath)
		})
	})
})
