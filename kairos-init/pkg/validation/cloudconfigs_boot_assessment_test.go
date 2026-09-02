package validation_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const grubCloudConfig = "08_grub.yaml"

// armsAssessment reports whether a command starts a new GRUB boot assessment.
func armsAssessment(command string) bool {
	return strings.Contains(command, "enable_boot_assessment=yes")
}

// clearsTentative reports whether a command resets the tentative sentinel.
// grub2-editenv writes an empty value to unset a variable.
func clearsTentative(command string) bool {
	for _, line := range strings.Split(command, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "grub2-editenv ") {
			continue
		}
		if strings.HasSuffix(line, "boot_assessment_tentative=") {
			return true
		}
	}
	return false
}

var _ = Describe("Bundled cloudconfigs boot assessment", func() {
	var config schema.YipConfig

	BeforeEach(func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", grubCloudConfig))
		Expect(err).NotTo(HaveOccurred(), "read %s", grubCloudConfig)
		Expect(yaml.Unmarshal(content, &config)).To(Succeed(), "parse %s", grubCloudConfig)
	})

	// A failed upgrade leaves boot_assessment_tentative set, and the boot.before
	// step that clears the sentinels only runs from the active image. Arming a
	// new assessment without clearing it makes GRUB pick the fallback entry on
	// the next boot without ever trying the image the upgrade just wrote.
	It("clears the tentative sentinel wherever it arms a new assessment", func() {
		armed := false

		for stageName, steps := range config.Stages {
			for _, step := range steps {
				for _, command := range step.Commands {
					if !armsAssessment(command) {
						continue
					}
					armed = true
					Expect(clearsTentative(command)).To(BeTrue(),
						"%s: stage %q step %q arms boot assessment but never clears boot_assessment_tentative",
						grubCloudConfig, stageName, step.Name)
				}
			}
		}

		Expect(armed).To(BeTrue(),
			"%s: no step arms boot assessment, the guard above checks nothing", grubCloudConfig)
	})

	It("arms a new assessment after every upgrade", func() {
		steps, ok := config.Stages["after-upgrade"]
		Expect(ok).To(BeTrue(), "%s: missing after-upgrade stage", grubCloudConfig)

		found := false
		for _, step := range steps {
			for _, command := range step.Commands {
				if armsAssessment(command) {
					found = true
				}
			}
		}

		Expect(found).To(BeTrue(),
			"%s: after-upgrade must arm boot assessment for the image it just wrote", grubCloudConfig)
	})
})
