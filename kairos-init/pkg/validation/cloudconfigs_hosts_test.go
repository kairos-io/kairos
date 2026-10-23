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
	Name               string           `yaml:"name"`
	If                 string           `yaml:"if"`
	OnlyServiceManager string           `yaml:"only_service_manager"`
	Directories        []hostsDirectory `yaml:"directories"`
	Files              []hostsFile      `yaml:"files"`
	Commands           []string         `yaml:"commands"`
	Hostname           string           `yaml:"hostname"`
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

// rewriteHostsPaths points the absolute paths the stage touches at a
// temporary root. The substitution is done through placeholders because
// /usr/local/etc/hosts contains /etc/hosts as a substring, so a plain
// left-to-right ReplaceAll rewrites the persistent path twice.
func rewriteHostsPaths(s, root string) string {
	s = strings.ReplaceAll(s, "/usr/local/etc", "\x01P")
	s = strings.ReplaceAll(s, "/usr/local/sbin", "\x01S")
	s = strings.ReplaceAll(s, "/run/cos", "\x01R")
	s = strings.ReplaceAll(s, "/etc", "\x01E")
	s = strings.ReplaceAll(s, "\x01P", filepath.Join(root, "usr", "local", "etc"))
	s = strings.ReplaceAll(s, "\x01S", filepath.Join(root, "usr", "local", "sbin"))
	s = strings.ReplaceAll(s, "\x01R", filepath.Join(root, "run", "cos"))
	s = strings.ReplaceAll(s, "\x01E", filepath.Join(root, "etc"))
	return s
}

// bootHosts replays one boot of the initramfs stage on a systemd node.
func bootHosts(root string, steps []hostsStep) int {
	return bootHostsWith(root, steps, "systemd")
}

// bootHostsWith replays one boot of the initramfs stage against root, as the
// given service manager sees it.
//
// /etc is an ephemeral overlay on a Kairos node (RW_PATHS in 00_rootfs.yaml),
// so every boot starts from the image's copy again: the directory is recreated
// and seeded with a plain file here for the same reason. /usr/local is the
// persistent partition and is carried over between calls.
//
// The plugin order is yip's own: directories, then files, then commands, then
// the hostname key (pkg/executor/executor.go). Files are written with a
// truncating create, which is what yip's writeFile does. It returns how many
// steps passed their guard, so a spec can tell "the entry survived" from
// "nothing ran".
func bootHostsWith(root string, steps []hostsStep, serviceManager string) int {
	etc := filepath.Join(root, "etc")
	Expect(os.RemoveAll(etc)).To(Succeed())
	Expect(os.MkdirAll(etc, 0755)).To(Succeed())
	// What a distro base image ships, which is what the overlay's lower layer
	// hands back on every boot. /etc/hostname is empty rather than absent
	// because kairos-init truncates it at build time (pkg/stages/steps_init.go,
	// "truncate hostname").
	Expect(os.WriteFile(filepath.Join(etc, "hosts"), []byte("127.0.0.1 localhost\n"), 0644)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(etc, "hostname"), nil, 0644)).To(Succeed())
	Expect(os.MkdirAll(filepath.Join(root, "run", "cos"), 0755)).To(Succeed())

	applied := 0
	for _, step := range steps {
		if step.OnlyServiceManager != "" && step.OnlyServiceManager != serviceManager {
			continue
		}
		if step.If != "" && !guardPassesExpr(rewriteHostsPaths(step.If, root)) {
			continue
		}
		applied++

		for _, dir := range step.Directories {
			path := rewriteHostsPaths(dir.Path, root)
			Expect(os.MkdirAll(path, os.FileMode(dir.Permissions))).To(Succeed())
			Expect(os.Chmod(path, os.FileMode(dir.Permissions))).To(Succeed())
		}
		for _, file := range step.Files {
			path := rewriteHostsPaths(file.Path, root)
			content := file.Content
			// A file the node executes names the same absolute paths the
			// stage does, and has to reach the temporary root as well. The
			// unit and init files are read by a service manager rather than
			// run, so they are written exactly as they ship.
			if os.FileMode(file.Permissions)&0111 != 0 {
				content = rewriteHostsPaths(content, root)
			}
			Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
			Expect(os.WriteFile(path, []byte(content), os.FileMode(file.Permissions))).To(Succeed())
		}
		for _, command := range step.Commands {
			out, err := exec.Command("sh", "-c", rewriteHostsPaths(command, root)).CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), "step %q command %q failed: %s", step.Name, command, out)
		}
		if step.Hostname != "" {
			applyHostname(root, step.Hostname)
		}
	}
	return applied
}

// applyHostname is yip's hostname key: it writes the name into /etc/hostname
// through whatever that path is, a symlink included, and rewrites the
// 127.0.0.1 line of /etc/hosts to carry it, leaving every other line alone
// (pkg/plugins/hostname.go, SystemHostname and UpdateHostsFile).
//
// The name is used as it is written in the YAML. The
// kairos-{{ trunc 4 .MachineID }} template is deliberately not expanded: no
// assertion here depends on the generated value, and expanding it would tie
// the specs to the machine running them.
func applyHostname(root, hostname string) {
	Expect(os.WriteFile(filepath.Join(root, "etc", "hostname"), []byte(hostname+"\n"), 0644)).To(Succeed())

	hosts := filepath.Join(root, "etc", "hosts")
	content, err := os.ReadFile(hosts)
	Expect(err).NotTo(HaveOccurred(), "the hostname key reads /etc/hosts")

	rewritten := ""
	for _, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
		line = strings.TrimSpace(line)
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "127.0.0.1" {
			rewritten += "127.0.0.1 localhost " + hostname + "\n"
			continue
		}
		rewritten += line + "\n"
	}
	Expect(os.WriteFile(hosts, []byte(rewritten), 0644)).To(Succeed())
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

		Expect(bootHostsWith(root, steps, "systemd")).To(Equal(0))
		Expect(hostsPersistentPath(root)).NotTo(BeAnExistingFile())
	})
})

func hostnamePersistentPath(root string) string {
	return filepath.Join(root, "usr", "local", "etc", "hostname")
}

func runtimeHostname(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "etc", "hostname"))
	Expect(err).NotTo(HaveOccurred(), "/etc/hostname must exist after a boot")
	return strings.TrimSpace(string(content))
}

func savedHostname(root string) string {
	content, err := os.ReadFile(hostnamePersistentPath(root))
	Expect(err).NotTo(HaveOccurred(), "the persistent hostname file must exist after a boot")
	return strings.TrimSpace(string(content))
}

// hostsStepFileContent returns the content the stage writes to path, so a spec
// can run what the node will actually run instead of a restatement of it.
func hostsStepFileContent(steps []hostsStep, path string) string {
	for _, step := range steps {
		for _, file := range step.Files {
			if file.Path == path {
				return file.Content
			}
		}
	}
	Fail("31_hosts.yaml writes no file at " + path)
	return ""
}

// setHostnameAtRuntime models what systemd-hostnamed does on
// `hostnamectl set-hostname`: WRITE_STRING_FILE_ATOMIC (hostname/hostnamed.c)
// writes a temporary file next to the target and renames over it
// (basic/fileio.c, write_string_file_atomic_at, "we want replacement
// semantics here, i.e. rename() replaces but linkat() doesn't"). rename()
// replaces whatever is at the path, a symlink included.
func setHostnameAtRuntime(root, name string) {
	etc := filepath.Join(root, "etc")
	temporary := filepath.Join(etc, ".#hostnameXXXX")
	Expect(os.WriteFile(temporary, []byte(name+"\n"), 0644)).To(Succeed())
	Expect(os.Rename(temporary, filepath.Join(etc, "hostname"))).To(Succeed())
}

// imagePATH is the PATH the save runs under here: a directory holding the
// utilities a Kairos image is known to carry, and nothing else.
//
// The save script must work with the shell and cat alone. It was first written
// with cmp, which the images do not ship, and the missing binary cost nothing
// visible: `cmp -s a b || cp` takes the second branch when cmp cannot run, so
// the guard fell open, every trigger copied, and the only trace was a
// `cmp: command not found` in the journal. A full PATH hides that, because the
// host running these specs has cmp.
//
// Adding a name to this list is a claim that the images carry it. Check the
// image before you make it.
func imagePATH() string {
	directory := GinkgoT().TempDir()
	for _, tool := range []string{"cat"} {
		resolved, err := exec.LookPath(tool)
		Expect(err).NotTo(HaveOccurred(), "this test host has no %s", tool)
		Expect(os.Symlink(resolved, filepath.Join(directory, tool))).To(Succeed())
	}
	return directory
}

// runSaveScript runs command under a shell that can reach nothing but
// imagePATH, and holds it to saying nothing at all: on a node every line it
// prints is a line in the journal, and a `command not found` there is the only
// sign that a guard never ran.
func runSaveScript(root, command string) {
	runner := exec.Command("sh", "-c", rewriteHostsPaths(command, root))
	runner.Env = append(os.Environ(), "PATH="+imagePATH())

	out, err := runner.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "the save failed: %s", out)
	Expect(string(out)).To(BeEmpty(), "the save printed something, which on a node is a journal line")
}

// persistScriptPath is where the stage installs the one copy of the save that
// both service managers call.
const persistScriptPath = "/usr/local/sbin/kairos-persist-hostname"

// runPersistUnit runs the ExecStart line of the systemd service the stage
// installs, which is what the path unit triggers when /etc/hostname changes.
func runPersistUnit(root string, steps []hostsStep) {
	unit := hostsStepFileContent(steps, "/etc/systemd/system/kairos-persist-hostname.service")

	command := ""
	for _, line := range strings.Split(unit, "\n") {
		if rest, found := strings.CutPrefix(line, "ExecStart="); found {
			command = strings.TrimSpace(rest)
		}
	}
	Expect(command).NotTo(BeEmpty(), "the service has no ExecStart to run")

	runSaveScript(root, command)
}

// runOpenRCStop sources the init script the stage installs and calls its
// stop(), which is what OpenRC runs on shutdown.
func runOpenRCStop(root string, steps []hostsStep) {
	script := rewriteHostsPaths(hostsStepFileContent(steps, "/etc/init.d/kairos-persist-hostname"), root)
	path := filepath.Join(GinkgoT().TempDir(), "kairos-persist-hostname")
	Expect(os.WriteFile(path, []byte(script), 0755)).To(Succeed())

	runSaveScript(root, ". "+path+"; stop")
}

var _ = Describe("Bundled cloudconfigs persistent /etc/hostname", func() {
	var steps []hostsStep

	BeforeEach(func() {
		steps = readHostsSteps()
	})

	It("seeds a name on a first boot and saves it on the persistent partition", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		Expect(savedHostname(root)).NotTo(BeEmpty(), "the first boot must leave a name behind")
		Expect(runtimeHostname(root)).To(Equal(savedHostname(root)))
	})

	// The reason the symlink had to go: systemd watches the inode a path unit
	// resolves to, so while /etc/hostname is a symlink the rename that
	// replaces it reaches no watch (core/path.c, primary_wd) and nothing
	// copies the new name out.
	It("leaves /etc/hostname a plain file, not a symlink", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		info, err := os.Lstat(filepath.Join(root, "etc", "hostname"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode()&os.ModeSymlink).To(BeZero(),
			"a path unit cannot watch /etc/hostname while it is a symlink")
	})

	// /etc is ephemeral, so the overlay hands back the image's empty file on
	// the next boot and the saved name has to be put back.
	It("restores the saved name on every boot", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		first := savedHostname(root)

		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		Expect(runtimeHostname(root)).To(Equal(first))
		Expect(savedHostname(root)).To(Equal(first), "the saved name was reseeded")
	})

	// What #5461 fixed, kept here: a name written straight to the persistent
	// file is not overwritten by the generated default.
	It("keeps a name written to the persistent file across a reboot", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		Expect(os.WriteFile(hostnamePersistentPath(root), []byte("node1\n"), 0644)).To(Succeed())
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		Expect(savedHostname(root)).To(Equal("node1"))
		Expect(runtimeHostname(root)).To(Equal("node1"))
	})

	// The defect this change fixes. The rename replaces /etc/hostname, the
	// path unit sees it and the service copies it out, so the reboot keeps it.
	It("keeps a hostname set with hostnamectl across a reboot", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		setHostnameAtRuntime(root, "node1")
		runPersistUnit(root, steps)
		Expect(savedHostname(root)).To(Equal("node1"), "the service did not copy the new name out")

		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		Expect(runtimeHostname(root)).To(Equal("node1"))
	})

	// Without the unit the name is still lost, which is what the ticket
	// reported. This is the control for the spec above.
	It("loses a hostname set with hostnamectl when nothing copies it out", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		generated := savedHostname(root)

		setHostnameAtRuntime(root, "node1")
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		Expect(runtimeHostname(root)).To(Equal(generated))
	})

	// The service must not fire on its own writes, or it would fight the
	// restore step on a node where the two files already agree.
	It("does nothing when the two files already agree", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		generated := savedHostname(root)

		runPersistUnit(root, steps)
		Expect(savedHostname(root)).To(Equal(generated))
	})

	// The units live on /etc/systemd/system, which is a bind mount from the
	// persistent partition, so they are still there after a rollback to an
	// image that symlinks /etc/hostname at the persistent file. Writing
	// through that symlink writes the persistent file, and a save that copied
	// anyway would read and truncate the same inode, leaving the node with no
	// name at all.
	It("does not empty the saved name when /etc/hostname is a symlink to it", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		runtime := filepath.Join(root, "etc", "hostname")
		Expect(os.Remove(runtime)).To(Succeed())
		Expect(os.Symlink(hostnamePersistentPath(root), runtime)).To(Succeed())
		Expect(os.WriteFile(runtime, []byte("node1\n"), 0644)).To(Succeed())

		runPersistUnit(root, steps)
		Expect(savedHostname(root)).To(Equal("node1"))
	})

	// Both service managers run the same file, so there is one implementation
	// of the save to read and to fix.
	It("installs one save script that the units call", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		info, err := os.Stat(filepath.Join(root, "usr", "local", "sbin", "kairos-persist-hostname"))
		Expect(err).NotTo(HaveOccurred(), "the units call a script that is not installed")
		Expect(info.Mode().Perm()&0111).NotTo(BeZero(), "the save script is not executable")

		unit := hostsStepFileContent(steps, "/etc/systemd/system/kairos-persist-hostname.service")
		Expect(unit).To(ContainSubstring(persistScriptPath))
		openrc := hostsStepFileContent(steps, "/etc/init.d/kairos-persist-hostname")
		Expect(openrc).To(ContainSubstring(persistScriptPath))
	})

	// An empty /etc/hostname is what the overlay hands back before the
	// restore step runs. Copying that out would wipe the saved name.
	It("never saves an empty name over the saved one", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		generated := savedHostname(root)

		Expect(os.WriteFile(filepath.Join(root, "etc", "hostname"), nil, 0644)).To(Succeed())
		runPersistUnit(root, steps)

		Expect(savedHostname(root)).To(Equal(generated))
	})

	// openrc has no inotify, so it saves on shutdown instead.
	It("saves a hostname set at runtime when OpenRC stops the service", func() {
		root := GinkgoT().TempDir()
		Expect(bootHostsWith(root, steps, "openrc")).To(BeNumerically(">", 0))

		setHostnameAtRuntime(root, "node1")
		runOpenRCStop(root, steps)
		Expect(savedHostname(root)).To(Equal("node1"))

		Expect(bootHostsWith(root, steps, "openrc")).To(BeNumerically(">", 0))
		Expect(runtimeHostname(root)).To(Equal("node1"))
	})

	It("installs the OpenRC service in the default runlevel", func() {
		root := GinkgoT().TempDir()
		Expect(bootHostsWith(root, steps, "openrc")).To(BeNumerically(">", 0))

		link := filepath.Join(root, "etc", "runlevels", "default", "kairos-persist-hostname")
		info, err := os.Lstat(link)
		Expect(err).NotTo(HaveOccurred(), "the service is never run if it is not in a runlevel")
		Expect(info.Mode() & os.ModeSymlink).NotTo(BeZero())
	})

	It("pulls the path unit into multi-user.target", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		link := filepath.Join(root, "etc", "systemd", "system", "multi-user.target.wants", "kairos-persist-hostname.path")
		info, err := os.Lstat(link)
		Expect(err).NotTo(HaveOccurred(), "the path unit never starts if nothing wants it")
		Expect(info.Mode() & os.ModeSymlink).NotTo(BeZero())
	})

	// An empty persistent file is what a boot interrupted inside the save
	// step leaves behind. It is not a name, so seed over it.
	It("reseeds an empty persistent hostname", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		Expect(os.WriteFile(hostnamePersistentPath(root), nil, 0644)).To(Succeed())

		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))
		Expect(savedHostname(root)).NotTo(BeEmpty(), "an empty persistent hostname was kept")
	})

	// The file's own ordering rule: the hosts steps run first, so that the
	// name the seed sets lands on the persistent hosts file too.
	It("carries the seeded name into the persistent hosts file", func() {
		root := GinkgoT().TempDir()
		Expect(bootHosts(root, steps)).To(BeNumerically(">", 0))

		hosts, err := os.ReadFile(hostsPersistentPath(root))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(hosts)).To(ContainSubstring(savedHostname(root)),
			"the hostname key ran before /etc/hosts was a symlink")
	})

	// Recovery has no persistent partition mounted, so nothing may touch
	// either path there.
	It("touches nothing in recovery mode", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "run", "cos"), 0755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(root, "run", "cos", "recovery_mode"), nil, 0644)).To(Succeed())

		Expect(bootHostsWith(root, steps, "systemd")).To(Equal(0))
		Expect(bootHostsWith(root, steps, "openrc")).To(Equal(0))
		Expect(hostnamePersistentPath(root)).NotTo(BeAnExistingFile())
	})
})
