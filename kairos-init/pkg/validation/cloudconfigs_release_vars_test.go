package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const recoveryCloudConfig = "50_recovery.yaml"

// osReleaseVersion matches a read of the base image's VERSION, the key
// /etc/os-release uses for its own version. Hadron and Alpine do not set it at
// all, and where it is set it names the base distribution, never Kairos.
var osReleaseVersion = regexp.MustCompile(`\$\{?VERSION\}?([^_A-Za-z0-9]|$)`)

// cloudConfigDir is the directory holding the cloud-configs baked into every
// image.
func cloudConfigDir() string {
	return filepath.Join("..", "bundled", "cloudconfigs")
}

func readCloudConfig(name string) schema.YipConfig {
	var config schema.YipConfig
	content, err := os.ReadFile(filepath.Join(cloudConfigDir(), name))
	Expect(err).NotTo(HaveOccurred(), "read %s", name)
	Expect(yaml.Unmarshal(content, &config)).To(Succeed(), "parse %s", name)
	return config
}

var _ = Describe("Bundled cloudconfigs version reads", func() {
	// The Kairos version lives in /etc/kairos-release, and every key that file
	// carries is KAIROS_ prefixed. A stage that sources it and then reads
	// $VERSION gets the base image's version instead, or nothing at all on a
	// base that does not set one.
	It("never takes a version from os-release", func() {
		entries, err := os.ReadDir(cloudConfigDir())
		Expect(err).NotTo(HaveOccurred())

		checked := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			checked++
			config := readCloudConfig(entry.Name())
			for stageName, steps := range config.Stages {
				for _, step := range steps {
					for _, command := range step.Commands {
						Expect(osReleaseVersion.MatchString(command)).To(BeFalse(),
							"%s: stage %q step %q reads $VERSION, which only /etc/os-release defines. Source /etc/kairos-release and read $KAIROS_VERSION instead",
							entry.Name(), stageName, step.Name)
					}
				}
			}
		}
		Expect(checked).To(BeNumerically(">", 1), "no cloud-configs were read")
	})

	// The banner is the only place an operator in recovery mode is told which
	// version a reset restores, so it has to name the Kairos one.
	It("names the Kairos version in the recovery banner", func() {
		config := readCloudConfig(recoveryCloudConfig)

		found := false
		for _, step := range config.Stages["boot"] {
			for _, command := range step.Commands {
				if !strings.Contains(command, "/etc/issue") {
					continue
				}
				found = true
				Expect(command).To(ContainSubstring("/etc/kairos-release"),
					"the recovery banner must source /etc/kairos-release")
				Expect(command).To(ContainSubstring("KAIROS_VERSION"),
					"the recovery banner must report KAIROS_VERSION")
			}
		}
		Expect(found).To(BeTrue(), "%s writes no recovery banner to /etc/issue", recoveryCloudConfig)
	})
})
