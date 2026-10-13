package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type hostsDirectory struct {
	Path        string `yaml:"path"`
	Permissions uint32 `yaml:"permissions"`
}

type hostsFile struct {
	Path        string `yaml:"path"`
	Permissions uint32 `yaml:"permissions"`
	Content     string `yaml:"content"`
}

type hostsStep struct {
	Name        string           `yaml:"name"`
	If          string           `yaml:"if"`
	Directories []hostsDirectory `yaml:"directories"`
	Files       []hostsFile      `yaml:"files"`
	Commands    []string         `yaml:"commands"`
	Hostname    string           `yaml:"hostname"`
}

type hostsConfig struct {
	Stages map[string][]hostsStep `yaml:"stages"`
}

func readHostsSteps() []hostsStep {
	content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", "31_hosts.yaml"))
	Expect(err).NotTo(HaveOccurred(), "read 31_hosts.yaml")

	var cfg hostsConfig
	Expect(yaml.Unmarshal(content, &cfg)).To(Succeed(), "parse 31_hosts.yaml")
	Expect(cfg.Stages["initramfs"]).NotTo(BeEmpty(), "no initramfs stage")
	return cfg.Stages["initramfs"]
}

// rewriteHostsPaths points the three absolute paths the stage touches at a
// temporary root. The substitution is done through placeholders because
// /usr/local/etc/hosts contains /etc/hosts as a substring, so a plain
// left-to-right ReplaceAll rewrites the persistent path twice.
func rewriteHostsPaths(s, root string) string {
	s = strings.ReplaceAll(s, "/usr/local/etc", "\x01P")
	s = strings.ReplaceAll(s, "/run/cos", "\x01R")
	s = strings.ReplaceAll(s, "/etc", "\x01E")
	s = strings.ReplaceAll(s, "\x01P", filepath.Join(root, "usr", "local", "etc"))
	s = strings.ReplaceAll(s, "\x01R", filepath.Join(root, "run", "cos"))
	s = strings.ReplaceAll(s, "\x01E", filepath.Join(root, "etc"))
	return s
}

// bootHosts replays one boot of the initramfs stage against root.
//
// /etc is an ephemeral overlay on a Kairos node (RW_PATHS in 00_rootfs.yaml),
// so every boot starts from the image's copy again: the directory is recreated
// and seeded with a plain file here for the same reason. /usr/local is the
// persistent partition and is carried over between calls.
//
// The plugin order is yip's own: directories, then files, then commands
// (pkg/executor/executor.go). Files are written with a truncating create,
// which is what yip's writeFile does. The hostname key is not modelled: it
// only rewrites the 127.0.0.1 line of /etc/hosts and leaves every other line
// alone (pkg/plugins/hostname.go UpdateHostsFile), so it cannot decide any of
// the assertions below. It returns how many steps passed their guard, so a
// spec can tell "the entry survived" from "nothing ran".
func bootHosts(root string, steps []hostsStep) int {
	etc := filepath.Join(root, "etc")
	Expect(os.RemoveAll(etc)).To(Succeed())
	Expect(os.MkdirAll(etc, 0755)).To(Succeed())
	// What a distro base image ships, which is what the overlay's lower layer
	// hands back on every boot.
	Expect(os.WriteFile(filepath.Join(etc, "hosts"), []byte("127.0.0.1 localhost\n"), 0644)).To(Succeed())
	Expect(os.MkdirAll(filepath.Join(root, "run", "cos"), 0755)).To(Succeed())

	applied := 0
	for _, step := range steps {
		if step.If != "" {
			if err := exec.Command("sh", "-c", rewriteHostsPaths(step.If, root)).Run(); err != nil {
				continue
			}
		}
		applied++

		for _, dir := range step.Directories {
			path := rewriteHostsPaths(dir.Path, root)
			Expect(os.MkdirAll(path, os.FileMode(dir.Permissions))).To(Succeed())
			Expect(os.Chmod(path, os.FileMode(dir.Permissions))).To(Succeed())
		}
		for _, file := range step.Files {
			path := rewriteHostsPaths(file.Path, root)
			Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
			Expect(os.WriteFile(path, []byte(file.Content), os.FileMode(file.Permissions))).To(Succeed())
		}
		for _, command := range step.Commands {
			out, err := exec.Command("sh", "-c", rewriteHostsPaths(command, root)).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "step %q command %q failed: %s", step.Name, command, out)
		}
	}
	return applied
}

func hostsPersistentPath(root string) string {
	return filepath.Join(root, "usr", "local", "etc", "hosts")
}

var _ = Describe("Bundled cloudconfigs persistent /etc/hosts", func() {
	var steps []hostsStep

	BeforeEach(func() {
		steps = readHostsSteps()
	})

	It("seeds the persistent file and links /etc/hosts to it on a first boot", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		content, err := os.ReadFile(hostsPersistentPath(root))
		Expect(err).NotTo(HaveOccurred(), "the persistent hosts file must exist after the first boot")
		Expect(string(content)).To(ContainSubstring("127.0.0.1 localhost"))

		target, err := os.Readlink(filepath.Join(root, "etc", "hosts"))
		Expect(err).NotTo(HaveOccurred(), "/etc/hosts must be a symlink after the first boot")
		Expect(target).To(Equal(hostsPersistentPath(root)))
	})

	// /etc is ephemeral, so the symlink is gone again on the next boot and has
	// to be recreated. This is the half that must keep running every time.
	It("relinks /etc/hosts on every boot", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		target, err := os.Readlink(filepath.Join(root, "etc", "hosts"))
		Expect(err).NotTo(HaveOccurred(), "/etc/hosts must be a symlink again after the second boot")
		Expect(target).To(Equal(hostsPersistentPath(root)))
	})

	// The whole point of the file: an entry written through the symlink lands
	// on the persistent partition, and must still be there after a reboot.
	It("keeps an entry added at runtime across a reboot", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		handle, err := os.OpenFile(filepath.Join(root, "etc", "hosts"), os.O_APPEND|os.O_WRONLY, 0644)
		Expect(err).NotTo(HaveOccurred())
		_, err = handle.WriteString("10.0.0.5 registry.internal\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(handle.Close()).To(Succeed())

		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		content, err := os.ReadFile(hostsPersistentPath(root))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("10.0.0.5 registry.internal"),
			"the persistent hosts file was rewritten from the built-in default")
	})

	// Recovery has no persistent partition mounted, so nothing may touch
	// either path there.
	It("touches nothing in recovery mode", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "run", "cos"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "run", "cos", "recovery_mode"), nil, 0644)).To(Succeed())

		Expect(bootHosts(root, steps)).To(Equal(0))
		Expect(hostsPersistentPath(root)).NotTo(BeAnExistingFile())
	})
})
