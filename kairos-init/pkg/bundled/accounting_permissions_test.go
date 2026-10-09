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

// permissionStage is one "Ensure runtime permission" stage of the shipped
// 10_accounting.yaml, with the guard and the commands exactly as the image
// runs them at boot.
type permissionStage struct {
	path     string
	guard    string
	commands []string
}

// permissionStages pulls the stages out of the shipped cloud-config and pairs
// each with the path it manages, taken from the guard rather than restated
// here, so a stage that starts managing a different path is still covered.
func permissionStages() []permissionStage {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/10_accounting.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages struct {
			Initramfs []struct {
				Name     string   `yaml:"name"`
				If       string   `yaml:"if"`
				Commands []string `yaml:"commands"`
			} `yaml:"initramfs"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	var stages []permissionStage
	for _, stage := range config.Stages.Initramfs {
		if stage.Name != "Ensure runtime permission" {
			continue
		}
		stages = append(stages, permissionStage{
			path:     pathUnderTest(stage.If),
			guard:    stage.If,
			commands: stage.Commands,
		})
	}

	Expect(stages).To(HaveLen(2), "10_accounting.yaml no longer has the two permission stages")
	return stages
}

// pathUnderTest reads the absolute path out of a guard such as
// `[ -d "/oem" ] && [ ! -L "/oem" ]`.
func pathUnderTest(guard string) string {
	_, rest, found := strings.Cut(guard, `"`)
	Expect(found).To(BeTrue(), "no quoted path in guard %q", guard)
	path, _, found := strings.Cut(rest, `"`)
	Expect(found).To(BeTrue(), "unterminated path in guard %q", guard)
	return path
}

// run executes the stage against root instead of /, by pointing the managed
// path at a directory under root. The guard and the commands are the shipped
// text with that one substitution, so the shell the node runs is the shell the
// spec runs.
//
// chown needs root to succeed and the specs do not run as root, so each
// command runs on its own and a chown failure is tolerated. What the specs
// assert is the mode, which is what the reported bug changes.
func (s permissionStage) run(root string) {
	target := filepath.Join(root, s.path)

	script := "if " + strings.ReplaceAll(s.guard, s.path, target) + "; then\n"
	for _, command := range s.commands {
		script += strings.ReplaceAll(command, s.path, target) + " || true\n"
	}
	script += "fi\n"

	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), "stage failed: %s", string(out))
}

func modeOf(path string) os.FileMode {
	info, err := os.Stat(path)
	Expect(err).ToNot(HaveOccurred())
	return info.Mode().Perm()
}

var _ = Describe("Accounting runtime permissions", func() {
	// The stage exists to make the directory group-writable for the admin
	// group, so it has to keep doing that on a node that is not under attack.
	It("sets the mode on a real directory", func() {
		for _, stage := range permissionStages() {
			root := GinkgoT().TempDir()
			managed := filepath.Join(root, stage.path)
			Expect(os.MkdirAll(managed, 0o755)).To(Succeed())

			stage.run(root)

			Expect(modeOf(managed)).To(Equal(os.FileMode(0o770)),
				"%s did not get its mode", stage.path)
		}
	})

	// The bug: chmod has no -h on Linux, so with the managed path replaced by
	// a symlink the mode landed on whatever the link pointed at. Pointing it at
	// a stand-in /etc made that directory 0770 on every boot, which denies bash
	// and sshd to every non-root user.
	It("leaves the target alone when the managed path is a symlink", func() {
		for _, stage := range permissionStages() {
			root := GinkgoT().TempDir()
			managed := filepath.Join(root, stage.path)
			Expect(os.MkdirAll(filepath.Dir(managed), 0o755)).To(Succeed())

			victim := filepath.Join(root, "etc")
			Expect(os.MkdirAll(victim, 0o755)).To(Succeed())
			Expect(os.Symlink(victim, managed)).To(Succeed())

			stage.run(root)

			Expect(modeOf(victim)).To(Equal(os.FileMode(0o755)),
				"%s followed a symlink and changed the mode of its target", stage.path)
		}
	})
})
