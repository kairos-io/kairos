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

// cloudConfigDir is the path the shipped stage manages. The specs below never
// touch it, they run the stage against a directory of their own, but the text
// has to be found in the shipped commands for the substitution to happen.
const cloudConfigDir = "/usr/local/cloud-config"

// cloudConfigStage is the fs.after stage of 00_rootfs.yaml that manages
// /usr/local/cloud-config, with the guard and the commands exactly as the
// image runs them at boot.
type cloudConfigStage struct {
	guard    string
	commands []string
}

func shippedCloudConfigStage() cloudConfigStage {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/00_rootfs.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages struct {
			FsAfter []struct {
				Name     string   `yaml:"name"`
				If       string   `yaml:"if"`
				Commands []string `yaml:"commands"`
			} `yaml:"fs.after"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	var found []cloudConfigStage
	for _, stage := range config.Stages.FsAfter {
		if !strings.Contains(strings.Join(stage.Commands, "\n"), cloudConfigDir) {
			continue
		}
		found = append(found, cloudConfigStage{guard: stage.If, commands: stage.Commands})
	}

	Expect(found).To(HaveLen(1), "00_rootfs.yaml no longer has one fs.after stage for %s", cloudConfigDir)
	return found[0]
}

// runAgainst runs the stage the way yip does, against root instead of
// /usr/local/cloud-config: the guard first, and the commands only when the
// guard passes. Guard and commands are the shipped text with that one
// substitution, so the shell the node runs is the shell the spec runs.
//
// chown needs root and an admin group, and the specs have neither, so a
// failing chown is tolerated. It can only leave the directory owned by the
// user running the spec, which is no weaker than what the stage asks for.
func (s cloudConfigStage) runAgainst(root string) {
	if s.guard != "" {
		guard := exec.Command("sh", "-c", strings.ReplaceAll(s.guard, cloudConfigDir, root))
		if err := guard.Run(); err != nil {
			return
		}
	}

	for _, command := range s.commands {
		out, err := exec.Command("sh", "-c", strings.ReplaceAll(command, cloudConfigDir, root)).CombinedOutput()
		if strings.HasPrefix(strings.TrimSpace(command), "chown") {
			continue
		}
		Expect(err).ToNot(HaveOccurred(), "%s: %s", command, out)
	}
}

var _ = Describe("The cloud-config directory stage", func() {
	var root string

	BeforeEach(func() {
		// kairos-agent's runstage() calls MkdirAll on every cloud-init path
		// with constants.DirPerm, which is 0777 before the umask, so the
		// directory is already there and already 0755 when fs.after runs.
		// Start from that state, because it is the only state the stage ever
		// sees on a real boot.
		root = filepath.Join(GinkgoT().TempDir(), "cloud-config")
		Expect(os.MkdirAll(root, 0777)).To(Succeed())
		Expect(os.Chmod(root, 0755)).To(Succeed())
	})

	It("takes the world bits off a directory the agent already created", func() {
		shippedCloudConfigStage().runAgainst(root)

		info, err := os.Stat(root)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0770)),
			"the stage ran but left %s at %04o", root, info.Mode().Perm())
	})

	It("keeps the directory traversable by its group", func() {
		shippedCloudConfigStage().runAgainst(root)

		info, err := os.Stat(root)
		Expect(err).ToNot(HaveOccurred())
		// 0600 on a directory clears the execute bit, and then nothing but
		// root can traverse it, which takes away the access the admin group
		// is granted by 10_accounting.yaml.
		Expect(info.Mode().Perm() & 0010).To(Equal(os.FileMode(0010)),
			"%s is not traversable by its group at %04o", root, info.Mode().Perm())
	})

	It("still creates the directory when it is missing", func() {
		Expect(os.RemoveAll(root)).To(Succeed())

		shippedCloudConfigStage().runAgainst(root)

		info, err := os.Stat(root)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0770)))
	})

	It("can run twice, as it does on every boot", func() {
		stage := shippedCloudConfigStage()
		stage.runAgainst(root)
		stage.runAgainst(root)

		info, err := os.Stat(root)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0770)))
	})
})
