package validation_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type sslPrivateStage struct {
	Name     string   `yaml:"name"`
	If       string   `yaml:"if"`
	Commands []string `yaml:"commands"`
}

type sslPrivateConfig struct {
	Stages map[string][]sslPrivateStage `yaml:"stages"`
}

func readSSLPrivateStages() []sslPrivateStage {
	content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "01_ssl_private_perms.yaml"))
	Expect(err).NotTo(HaveOccurred(), "read 01_ssl_private_perms.yaml")

	var cfg sslPrivateConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed(), "parse 01_ssl_private_perms.yaml")
	Expect(cfg.Stages["initramfs"]).NotTo(BeEmpty(), "no initramfs stage")
	return cfg.Stages["initramfs"]
}

// runSSLPrivateStage executes the stage body the way yip does, with sh -c,
// against a temporary /etc/ssl/private and a temporary /etc/group. group is the
// literal /etc/group content, so a spec can decide whether ssl-cert exists.
// It returns the resulting directory mode.
func runSSLPrivateStage(command, group string, startMode os.FileMode) os.FileMode {
	root := GinkgoT().TempDir()

	groupPath := filepath.Join(root, "group")
	Expect(os.WriteFile(groupPath, []byte(group), 0644)).To(Succeed())

	dir := filepath.Join(root, "private")
	Expect(os.Mkdir(dir, startMode)).To(Succeed())
	// Mkdir applies the umask, so set the mode we asked for explicitly.
	Expect(os.Chmod(dir, startMode)).To(Succeed())

	command = strings.ReplaceAll(command, "/etc/group", groupPath)
	command = strings.ReplaceAll(command, "/etc/ssl/private", dir)
	// chown needs root, and these specs are about the mode. Neutralise it
	// rather than skipping the whole spec when the suite runs unprivileged.
	command = strings.ReplaceAll(command, "chown ", "true ")

	out, err := exec.Command("sh", "-c", command).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "stage body failed: %s", out)

	info, err := os.Stat(dir)
	Expect(err).NotTo(HaveOccurred())
	return info.Mode().Perm()
}

var _ = Describe("Bundled cloudconfigs /etc/ssl/private permissions", func() {
	var stage sslPrivateStage

	BeforeEach(func() {
		stages := readSSLPrivateStages()
		Expect(stages).To(HaveLen(1))
		stage = stages[0]
		Expect(stage.Commands).To(HaveLen(1))
	})

	It("only runs when the directory exists", func() {
		root := GinkgoT().TempDir()
		present := filepath.Join(root, "present")
		Expect(os.Mkdir(present, 0755)).To(Succeed())

		run := func(path string) bool {
			expr := strings.ReplaceAll(stage.If, "/etc/ssl/private", path)
			return exec.Command("sh", "-c", expr).Run() == nil
		}

		Expect(run(present)).To(BeTrue())
		Expect(run(filepath.Join(root, "absent"))).To(BeFalse())
	})

	// Hadron ships 0755 root:root and has no ssl-cert group, which is the case
	// this file exists for: without it that mode is copied into the persistent
	// backing directory and every later boot keeps it.
	It("tightens a world-traversable directory to 0700 with no ssl-cert group", func() {
		mode := runSSLPrivateStage(stage.Commands[0], "root:x:0:\nnogroup:x:65534:\n", 0755)
		Expect(mode).To(Equal(os.FileMode(0700)))
	})

	// Debian and Ubuntu: the group exists so the directory has to stay
	// traversable for it, and 0700 would break daemons that read a key there.
	It("keeps 0710 when the ssl-cert group exists", func() {
		mode := runSSLPrivateStage(stage.Commands[0], "root:x:0:\nssl-cert:x:115:\n", 0710)
		Expect(mode).To(Equal(os.FileMode(0710)))
	})

	It("tightens to 0710 when the ssl-cert group exists but the mode is loose", func() {
		mode := runSSLPrivateStage(stage.Commands[0], "root:x:0:\nssl-cert:x:115:\n", 0755)
		Expect(mode).To(Equal(os.FileMode(0710)))
	})

	// A group whose name merely contains ssl-cert, or a user of that name in
	// another group's member list, must not be read as the group itself.
	It("matches the ssl-cert group by name, not by substring", func() {
		for _, group := range []string{
			"root:x:0:\nmy-ssl-cert:x:115:\n",
			"root:x:0:\nsomething:x:115:ssl-cert\n",
		} {
			mode := runSSLPrivateStage(stage.Commands[0], group, 0755)
			Expect(mode).To(Equal(os.FileMode(0700)), fmt.Sprintf("group file %q", group))
		}
	})

	It("is idempotent", func() {
		body := stage.Commands[0]
		Expect(runSSLPrivateStage(body+"\n"+body, "root:x:0:\n", 0755)).To(Equal(os.FileMode(0700)))
	})
})
