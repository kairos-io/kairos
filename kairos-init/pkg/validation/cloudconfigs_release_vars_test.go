package validation_test

import (
	"fmt"
	"os"
	"os/exec"
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

func readCloudConfig(name string) schema.YipConfig {
	var config schema.YipConfig
	content, err := os.ReadFile(filepath.Join(cloudConfigsDir, name))
	Expect(err).NotTo(HaveOccurred(), "read %s", name)
	Expect(yaml.Unmarshal(content, &config)).To(Succeed(), "parse %s", name)
	return config
}

// shellStrings returns every string a stage hands to a shell or writes to disk,
// each labelled with where it came from so a failure names the right place.
func shellStrings(step schema.Stage) map[string]string {
	sources := map[string]string{}
	for i, command := range step.Commands {
		sources[fmt.Sprintf("command %d", i)] = command
	}
	if step.If != "" {
		sources["if"] = step.If
	}
	for _, file := range step.Files {
		sources["file "+file.Path] = file.Content
	}
	for key, value := range step.Environment {
		sources["environment "+key] = value
	}
	return sources
}

// recoveryBannerCommand returns the boot-stage command that writes the recovery
// banner, with /etc/kairos-release and /etc/issue pointed at dir so the script
// can be run for real.
func recoveryBannerCommand(dir string) string {
	config := readCloudConfig(recoveryCloudConfig)

	var found []string
	for _, step := range config.Stages["boot"] {
		for _, command := range step.Commands {
			if strings.Contains(command, "/etc/issue") {
				found = append(found, command)
			}
		}
	}
	Expect(found).To(HaveLen(1), "%s must write the recovery banner from exactly one command", recoveryCloudConfig)

	command := strings.ReplaceAll(found[0], "/etc/kairos-release", filepath.Join(dir, "kairos-release"))
	return strings.ReplaceAll(command, "/etc/issue", filepath.Join(dir, "issue"))
}

var _ = Describe("Bundled cloudconfigs version reads", func() {
	// The Kairos version lives in /etc/kairos-release, and every key that file
	// carries is KAIROS_ prefixed. A stage that sources it and then reads
	// $VERSION gets the base image's version instead, or nothing at all on a
	// base that does not set one.
	It("never takes a version from os-release", func() {
		entries, err := os.ReadDir(cloudConfigsDir)
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
					for source, text := range shellStrings(step) {
						Expect(osReleaseVersion.MatchString(text)).To(BeFalse(),
							"%s: stage %q step %q %s reads $VERSION, which only /etc/os-release defines. Source /etc/kairos-release and read $KAIROS_VERSION instead",
							entry.Name(), stageName, step.Name, source)
					}
				}
			}
		}
		Expect(checked).To(BeNumerically(">", 1), "no cloud-configs were read")
	})

	// The banner is the only place an operator in recovery mode is told which
	// version a reset restores, so it has to name the Kairos one. Run the
	// script rather than grepping it: the bug this guards against is a
	// shell-variable name, which a substring check cannot see.
	DescribeTable("writes the recovery banner",
		func(releaseFile string, expected string) {
			dir := GinkgoT().TempDir()
			if releaseFile != "" {
				Expect(os.WriteFile(filepath.Join(dir, "kairos-release"), []byte(releaseFile), 0o600)).To(Succeed())
			}

			out, err := exec.Command("sh", "-c", recoveryBannerCommand(dir)).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))

			issue, err := os.ReadFile(filepath.Join(dir, "issue"))
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.Split(string(issue), "\n")).To(ContainElement(expected))
		},
		Entry("with the Kairos version",
			"KAIROS_VERSION=\"v9.9.9\"\nKAIROS_ID=\"kairos\"\n",
			"You are booting from recovery mode. Run 'kairos-agent reset' to reset the system to v9.9.9"),
		Entry("with no kairos-release at all",
			"",
			"You are booting from recovery mode. Run 'kairos-agent reset' to reset the system"),
		Entry("with a kairos-release that sets no version",
			"KAIROS_ID=\"kairos\"\n",
			"You are booting from recovery mode. Run 'kairos-agent reset' to reset the system"),
	)
})
