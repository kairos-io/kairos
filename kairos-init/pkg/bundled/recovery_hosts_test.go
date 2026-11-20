package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const recoveryHostsStageName = "Seed the recovery hosts file"

// recoveryHostsStage is the stage of the shipped 50_recovery.yaml that makes
// sure /etc/hosts has a 127.0.0.1 line before the hostname plugin edits it.
func recoveryHostsStage() (guard string, commands []string) {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/50_recovery.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages struct {
			Boot []struct {
				Name     string   `yaml:"name"`
				If       string   `yaml:"if"`
				Commands []string `yaml:"commands"`
			} `yaml:"boot"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	for _, stage := range config.Stages.Boot {
		if stage.Name == recoveryHostsStageName {
			return stage.If, stage.Commands
		}
	}
	Fail("50_recovery.yaml has no stage named " + recoveryHostsStageName)
	return "", nil
}

// runRecoveryHosts runs the shipped guard and commands with /etc/hosts and the
// recovery sentinel pointed under root.
func runRecoveryHosts(root string, recovery bool) {
	guard, commands := recoveryHostsStage()
	sentinel := filepath.Join(root, "recovery_mode")
	if recovery {
		Expect(os.WriteFile(sentinel, nil, 0o644)).To(Succeed())
	}
	sub := strings.NewReplacer("/run/cos/recovery_mode", sentinel, "/etc/hosts", filepath.Join(root, "hosts"))

	script := "if " + sub.Replace(guard) + "; then\n" + sub.Replace(strings.Join(commands, "\n")) + "\nfi\n"
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "stage failed: %s", string(out))
}

var _ = Describe("Recovery hosts file", func() {
	var root, hosts string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		hosts = filepath.Join(root, "hosts")
	})

	It("seeds a loopback line when the hosts file is empty", func() {
		Expect(os.WriteFile(hosts, nil, 0o644)).To(Succeed())
		runRecoveryHosts(root, true)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(MatchRegexp(`(?m)^127\.0\.0\.1\s+localhost$`))
	})

	It("seeds a loopback line when the hosts file is missing", func() {
		runRecoveryHosts(root, true)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(MatchRegexp(`(?m)^127\.0\.0\.1\s+localhost$`))
	})

	It("keeps a hosts file that already has a 127.0.0.1 line", func() {
		content := "127.0.0.1 localhost custom\n"
		Expect(os.WriteFile(hosts, []byte(content), 0o644)).To(Succeed())
		runRecoveryHosts(root, true)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal(content))
	})

	It("seeds a loopback line when only a longer 127.0.0.1x address is present", func() {
		Expect(os.WriteFile(hosts, []byte("127.0.0.10 other\n"), 0o644)).To(Succeed())
		runRecoveryHosts(root, true)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(MatchRegexp(`(?m)^127\.0\.0\.1\s+localhost$`))
		Expect(string(data)).To(ContainSubstring("127.0.0.10 other\n"))
	})

	It("starts the seed on its own line when the file has no trailing newline", func() {
		Expect(os.WriteFile(hosts, []byte("192.0.2.1 other"), 0o644)).To(Succeed())
		runRecoveryHosts(root, true)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(HavePrefix("192.0.2.1 other\n127.0.0.1 localhost\n"))
	})

	It("keeps an indented 127.0.0.1 line", func() {
		content := "  127.0.0.1 localhost\n"
		Expect(os.WriteFile(hosts, []byte(content), 0o644)).To(Succeed())
		runRecoveryHosts(root, true)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal(content))
	})

	It("does nothing outside recovery mode", func() {
		Expect(os.WriteFile(hosts, nil, 0o644)).To(Succeed())
		runRecoveryHosts(root, false)
		data, err := os.ReadFile(hosts)
		Expect(err).ToNot(HaveOccurred())
		Expect(data).To(BeEmpty())
	})
})
