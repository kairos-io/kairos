package phonehome_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kairos-io/kairos/v4/agent/internal/phonehome"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fakeFS is the in-memory filesystem used by the Uninstall specs. The five
// package-local indirections in uninstall.go (runCommand, removeFile,
// readFile, writeFile, walkFiles) are swapped to close over this struct so
// each spec sets up its own precondition world without touching real /oem.
type fakeFS struct {
	systemctlCalls [][]string
	files          map[string][]byte
	removedPaths   []string
}

func newFakeFS(initial map[string]string) *fakeFS {
	f := &fakeFS{files: map[string][]byte{}}
	for k, v := range initial {
		f.files[k] = []byte(v)
	}
	return f
}

func (f *fakeFS) runCommand(name string, args ...string) ([]byte, error) {
	Expect(name).To(Equal("systemctl"))
	f.systemctlCalls = append(f.systemctlCalls, args)
	return nil, nil
}

func (f *fakeFS) removeFile(path string) error {
	if _, ok := f.files[path]; !ok {
		return os.ErrNotExist
	}
	delete(f.files, path)
	f.removedPaths = append(f.removedPaths, path)
	return nil
}

func (f *fakeFS) readFile(path string) ([]byte, error) {
	b, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}

func (f *fakeFS) writeFile(path string, data []byte, _ os.FileMode) error {
	f.files[path] = append([]byte{}, data...)
	return nil
}

func (f *fakeFS) walk(root string) ([]string, error) {
	var out []string
	for path := range f.files {
		if strings.HasPrefix(path, root+"/") {
			out = append(out, path)
		}
	}
	return out, nil
}

func (f *fakeFS) install(restore *func()) {
	r := phonehome.SetUninstallRunners(f.runCommand, f.removeFile, f.readFile, f.writeFile, f.walk)
	*restore = r
}

var _ = Describe("phonehome.Uninstall", func() {
	var (
		fs      *fakeFS
		restore func()
	)

	AfterEach(func() {
		if restore != nil {
			restore()
			restore = nil
		}
	})

	Describe("ordering and systemd lifecycle", func() {
		// The remote `unregister` handler runs inside the phone-home
		// service's own process, so stopService=false skips
		// `systemctl stop` — stopping would SIGTERM the process before it
		// could finish writing the Completed status over the WebSocket.
		It("skips `systemctl stop` when stopService is false", func() {
			fs = newFakeFS(map[string]string{
				phonehome.ServicePath:            "[Unit]\n",
				phonehome.DefaultCredentialsPath: "node_id: abc\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			var verbs []string
			for _, args := range fs.systemctlCalls {
				verbs = append(verbs, args[0])
			}
			Expect(verbs).ToNot(ContainElement("stop"))
			Expect(verbs).To(Equal([]string{"disable", "daemon-reload"}))
			Expect(fs.files).ToNot(HaveKey(phonehome.ServicePath))
			Expect(fs.files).ToNot(HaveKey(phonehome.DefaultCredentialsPath))
		})

		// The local CLI (`kairos-agent phone-home uninstall`) is a separate
		// process from the service, so stopping the unit here does the
		// right thing: it kills the running service, not the CLI.
		It("stops the service first when stopService is true", func() {
			fs = newFakeFS(map[string]string{
				phonehome.ServicePath:            "[Unit]\n",
				phonehome.DefaultCredentialsPath: "node_id: abc\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(true)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(fs.systemctlCalls).To(HaveLen(3))
			Expect(fs.systemctlCalls[0]).To(Equal([]string{"stop", phonehome.ServiceName}))
			Expect(fs.systemctlCalls[1]).To(Equal([]string{"disable", phonehome.ServiceName}))
			Expect(fs.systemctlCalls[2]).To(Equal([]string{"daemon-reload"}))
		})

		// Partial install (no unit file, no credentials) must not error —
		// the Decommission flow runs Uninstall on every delete, and a
		// second click on a clean node must converge silently.
		It("is idempotent on a partially-installed or already-clean node", func() {
			fs = newFakeFS(nil)
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)
			Expect(summary).To(ContainSubstring("already absent: " + phonehome.ServicePath))
			Expect(summary).To(ContainSubstring("already absent: " + phonehome.DefaultCredentialsPath))
		})
	})

	Describe("OEM cloud-config cleanup", func() {
		// Real Kairos auto-install merges every datasource + overlay
		// cloud-config into a single /oem/90_custom.yaml with other keys
		// alongside `phonehome:`. We must only strip the phonehome stanza
		// and leave the rest of the file intact, order and header preserved.
		It("strips the phonehome stanza from a merged /oem/90_custom.yaml", func() {
			before := `#cloud-config
install:
  auto: true
  device: /dev/vda
  reboot: true
phonehome:
  url: "http://example"
  registration_token: "tok"
  group: ""
stages:
  initramfs:
    - name: seed
`
			fs = newFakeFS(map[string]string{"/oem/90_custom.yaml": before})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			rewritten := string(fs.files["/oem/90_custom.yaml"])
			Expect(rewritten).ToNot(ContainSubstring("phonehome"))
			Expect(rewritten).ToNot(ContainSubstring("registration_token"))
			Expect(rewritten).To(HavePrefix("#cloud-config\n"))
			Expect(rewritten).To(ContainSubstring("install:"))
			Expect(rewritten).To(ContainSubstring("stages:"))
			Expect(strings.Index(rewritten, "install:")).To(BeNumerically("<", strings.Index(rewritten, "stages:")))
			Expect(summary).To(ContainSubstring("stripped phonehome stanza from /oem/90_custom.yaml"))
		})

		// The install-script's standalone /oem/phonehome.yaml holds only
		// the phonehome section. After stripping it the file is empty and
		// we should delete it rather than leave an empty config behind.
		It("removes the file when phonehome was its only content", func() {
			fs = newFakeFS(map[string]string{
				"/oem/phonehome.yaml": "#cloud-config\nphonehome:\n  url: \"x\"\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(fs.files).ToNot(HaveKey("/oem/phonehome.yaml"))
			Expect(summary).To(ContainSubstring("removed /oem/phonehome.yaml"))
		})

		It("leaves files without a phonehome key untouched", func() {
			original := "#cloud-config\nhostname: foo\n"
			fs = newFakeFS(map[string]string{"/oem/99_other.yaml": original})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(string(fs.files["/oem/99_other.yaml"])).To(Equal(original))
			Expect(summary).To(ContainSubstring("no phonehome stanza found under /oem or /usr/local/cloud-config"))
		})

		It("scans both .yaml and .yml extensions", func() {
			fs = newFakeFS(map[string]string{
				"/oem/a.yml": "#cloud-config\nphonehome:\n  url: x\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(fs.files).ToNot(HaveKey("/oem/a.yml"))
			Expect(summary).To(ContainSubstring("removed /oem/a.yml"))
		})

		// A malformed file anywhere in /oem shouldn't stop us from
		// cleaning up the other files; it's almost certainly unrelated.
		It("tolerates malformed YAML in unrelated /oem files", func() {
			fs = newFakeFS(map[string]string{
				"/oem/broken.yaml":    "this is: : not valid yaml: [",
				"/oem/90_custom.yaml": "#cloud-config\nphonehome:\n  url: x\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(fs.files).ToNot(HaveKey("/oem/90_custom.yaml"))
			Expect(summary).To(ContainSubstring("skipped /oem/broken.yaml"))
		})

		// The boot scan walks /oem recursively, so a phonehome stanza in a
		// subdirectory is just as live as one at the top level.
		It("cleans cloud-configs in /oem subdirectories", func() {
			fs = newFakeFS(map[string]string{
				"/oem/extra/nested/90_custom.yaml": "#cloud-config\nhostname: foo\nphonehome:\n  url: x\n",
				"/oem/extra/phonehome.yml":         "#cloud-config\nphonehome:\n  url: x\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(string(fs.files["/oem/extra/nested/90_custom.yaml"])).To(Equal("#cloud-config\nhostname: foo\n"))
			Expect(fs.files).ToNot(HaveKey("/oem/extra/phonehome.yml"))
			Expect(summary).To(ContainSubstring("stripped phonehome stanza from /oem/extra/nested/90_custom.yaml"))
			Expect(summary).To(ContainSubstring("removed /oem/extra/phonehome.yml"))
		})

		// /usr/local/cloud-config is the other writable directory the boot
		// scan reads; on nodes without an OEM partition it is where the
		// config lands.
		It("cleans cloud-configs under /usr/local/cloud-config", func() {
			fs = newFakeFS(map[string]string{
				"/usr/local/cloud-config/90_custom.yaml":     "#cloud-config\nhostname: foo\nphonehome:\n  url: x\n",
				"/usr/local/cloud-config/sub/phonehome.yaml": "#cloud-config\nphonehome:\n  url: x\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(string(fs.files["/usr/local/cloud-config/90_custom.yaml"])).To(Equal("#cloud-config\nhostname: foo\n"))
			Expect(fs.files).ToNot(HaveKey("/usr/local/cloud-config/sub/phonehome.yaml"))
			Expect(summary).To(ContainSubstring("stripped phonehome stanza from /usr/local/cloud-config/90_custom.yaml"))
			Expect(summary).To(ContainSubstring("removed /usr/local/cloud-config/sub/phonehome.yaml"))
		})

		// The boot scan only loads .yaml/.yml, so nothing else is touched.
		It("ignores files without a .yaml or .yml extension", func() {
			original := "phonehome:\n  url: x\n"
			fs = newFakeFS(map[string]string{"/oem/notes.txt": original})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(string(fs.files["/oem/notes.txt"])).To(Equal(original))
		})
	})

	Describe("sources the cleanup cannot edit", func() {
		It("warns when the kernel cmdline carries phonehome or config_url", func() {
			fs = newFakeFS(map[string]string{
				"/proc/cmdline": "BOOT_IMAGE=/vmlinuz phonehome.url=http://x phonehome.registration_token=secret kairos.config_url=http://y\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(summary).To(ContainSubstring("kernel cmdline sets phonehome.url, phonehome.registration_token, kairos.config_url; " + phonehome.ServiceName + " may come back on next boot"))
			Expect(summary).ToNot(ContainSubstring("secret"))
		})

		It("does not warn when the kernel cmdline has nothing relevant", func() {
			fs = newFakeFS(map[string]string{"/proc/cmdline": "BOOT_IMAGE=/vmlinuz console=ttyS0\n"})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(summary).ToNot(ContainSubstring("may come back"))
		})

		It("warns when a cloud-config pulls a remote config through config_url", func() {
			fs = newFakeFS(map[string]string{
				"/oem/90_custom.yaml": "#cloud-config\nconfig_url: http://y\nphonehome:\n  url: x\n",
			})
			fs.install(&restore)

			summary, err := phonehome.Uninstall(false)
			Expect(err).ToNot(HaveOccurred(), summary)

			Expect(string(fs.files["/oem/90_custom.yaml"])).To(Equal("#cloud-config\nconfig_url: http://y\n"))
			Expect(summary).To(ContainSubstring("/oem/90_custom.yaml sets config_url; " + phonehome.ServiceName + " may come back on next boot"))
		})
	})

	Describe("listing cloud-config files on disk", func() {
		It("walks recursively, follows file symlinks and skips non-regular files", func() {
			root := GinkgoT().TempDir()
			outside := GinkgoT().TempDir()
			Expect(os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(root, "top.yaml"), []byte("a: 1\n"), 0o600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(root, "a", "b", "deep.yml"), []byte("a: 1\n"), 0o600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(outside, "target.yaml"), []byte("a: 1\n"), 0o600)).To(Succeed())
			Expect(os.Symlink(filepath.Join(outside, "target.yaml"), filepath.Join(root, "link.yaml"))).To(Succeed())
			Expect(os.Symlink(outside, filepath.Join(root, "linkdir"))).To(Succeed())
			Expect(syscall.Mkfifo(filepath.Join(root, "fifo.yaml"), 0o600)).To(Succeed())

			files, err := phonehome.ListConfigFiles(root)
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(ConsistOf(
				filepath.Join(root, "top.yaml"),
				filepath.Join(root, "a", "b", "deep.yml"),
				filepath.Join(root, "link.yaml"),
			))
		})

		It("returns nothing for a missing directory", func() {
			files, err := phonehome.ListConfigFiles(filepath.Join(GinkgoT().TempDir(), "missing"))
			Expect(err).ToNot(HaveOccurred())
			Expect(files).To(BeEmpty())
		})
	})

	Describe("error surfacing", func() {
		// Permission-denied (running the CLI without sudo, a read-only
		// /etc, etc.) must surface so the operator knows the teardown
		// didn't converge — swallowing it would be worse than the
		// original "delete the DB row and leave the node hanging".
		It("surfaces a real filesystem error and names the offending path", func() {
			fs = newFakeFS(nil)
			// Override removeFile to simulate EACCES for the credentials file.
			permDenied := &os.PathError{Op: "remove", Path: phonehome.DefaultCredentialsPath, Err: os.ErrPermission}
			restore = phonehome.SetUninstallRunners(
				fs.runCommand,
				func(path string) error {
					if path == phonehome.DefaultCredentialsPath {
						return permDenied
					}
					return os.ErrNotExist
				},
				fs.readFile, fs.writeFile, fs.walk,
			)

			summary, err := phonehome.Uninstall(false)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, os.ErrPermission)).To(BeTrue())
			Expect(summary).To(ContainSubstring(phonehome.DefaultCredentialsPath))
		})
	})
})
