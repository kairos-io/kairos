package bundled_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"gopkg.in/yaml.v3"
)

// readOnlyUsrFS is an installed node's root as far as a directory plugin can
// tell it apart from any other: everything under /usr is image content and the
// kernel answers EROFS, except /usr/local, where COS_PERSISTENT is mounted.
// Embedding vfs.FS keeps every other call on the real temp directory.
type readOnlyUsrFS struct {
	vfs.FS
}

func (f readOnlyUsrFS) readOnly(path string) bool {
	clean := filepath.Clean(path)
	if clean == "/usr/local" || strings.HasPrefix(clean, "/usr/local/") {
		return false
	}
	return clean == "/usr" || strings.HasPrefix(clean, "/usr/")
}

func (f readOnlyUsrFS) Mkdir(path string, perm os.FileMode) error {
	if f.readOnly(path) {
		return &os.PathError{Op: "mkdir", Path: path, Err: syscall.EROFS}
	}
	return f.FS.Mkdir(path, perm)
}

func (f readOnlyUsrFS) Chmod(path string, mode os.FileMode) error {
	if f.readOnly(path) {
		return &os.PathError{Op: "chmod", Path: path, Err: syscall.EROFS}
	}
	return f.FS.Chmod(path, mode)
}

// Chown is a no-op on the writable half. The stages ask for owner 0, which the
// agent gets for free at boot and a test process does not, and whether a
// directory ends up owned by root is not what is under test here.
func (f readOnlyUsrFS) Chown(path string, _, _ int) error {
	if f.readOnly(path) {
		return &os.PathError{Op: "chown", Path: path, Err: syscall.EROFS}
	}
	return nil
}

// installedRoot builds the writable half of an installed node inside a temp
// directory: the tmpfs overlays 00_rootfs.yaml asks for, the COS_PERSISTENT
// mount point, and the state paths immucore binds in from it.
func installedRoot() vfs.FS {
	root := GinkgoT().TempDir()
	for _, dir := range writablePaths() {
		Expect(os.MkdirAll(filepath.Join(root, dir), 0o755)).To(Succeed())
	}
	return readOnlyUsrFS{FS: vfs.NewPathFS(vfs.OSFS, root)}
}

// rootfsLayout is the active/passive arm of the shipped 00_rootfs.yaml, which
// is what decides where an installed node can be written to.
func rootfsLayout() (rwPaths, volumes, statePaths string) {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/00_rootfs.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages struct {
			Rootfs []struct {
				Environment struct {
					RWPaths    string `yaml:"RW_PATHS"`
					Volumes    string `yaml:"VOLUMES"`
					StatePaths string `yaml:"PERSISTENT_STATE_PATHS"`
				} `yaml:"environment"`
			} `yaml:"rootfs"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	for _, stage := range config.Stages.Rootfs {
		// The active/passive arm is the only one that names state paths.
		if stage.Environment.StatePaths != "" {
			return stage.Environment.RWPaths, stage.Environment.Volumes, stage.Environment.StatePaths
		}
	}
	Fail("no rootfs stage in 00_rootfs.yaml declares PERSISTENT_STATE_PATHS")
	return "", "", ""
}

// writablePaths is every prefix an installed node can create a directory
// under: the RW_PATHS overlays, the mount point of each VOLUMES entry, the
// PERSISTENT_STATE_PATHS binds, and the kernel filesystems that are always
// there and always writable.
func writablePaths() []string {
	rwPaths, volumes, statePaths := rootfsLayout()

	paths := append(strings.Fields(rwPaths), strings.Fields(statePaths)...)
	for _, volume := range strings.Fields(volumes) {
		// VOLUMES entries are LABEL=NAME:/mount/point.
		if _, mountpoint, found := strings.Cut(volume, ":"); found {
			paths = append(paths, mountpoint)
		}
	}
	paths = append(paths, "/dev", "/run", "/sys", "/tmp")

	sort.Strings(paths)
	return paths
}

// shippedStages returns every stage of every cloud-config kairos-init writes
// into /system/oem, parsed by yip itself rather than by a struct of our own,
// so a stage this test cannot see is a stage yip cannot run either.
func shippedStages() map[string][]schema.Stage {
	stages := map[string][]schema.Stage{}

	files, err := bundled.EmbeddedConfigs.ReadDir("cloudconfigs")
	Expect(err).ToNot(HaveOccurred())

	for _, file := range files {
		raw, err := bundled.EmbeddedConfigs.ReadFile(filepath.Join("cloudconfigs", file.Name()))
		Expect(err).ToNot(HaveOccurred())

		config, err := schema.Load(string(raw), vfs.OSFS, nil, nil)
		Expect(err).ToNot(HaveOccurred(), file.Name())

		for name, stage := range config.Stages {
			stages[file.Name()+" "+name] = append(stages[file.Name()+" "+name], stage...)
		}
	}
	return stages
}

var _ = Describe("Shipped cloud-config directories", func() {
	// /usr is image content and read-only once the rootfs is in place. A
	// directory: entry pointing there cannot succeed on an installed node, and
	// yip reports the whole stage as failed for it on every boot. See #5140.
	It("only asks for directories an installed node can write to", func() {
		writable := writablePaths()

		for name, stages := range shippedStages() {
			for _, stage := range stages {
				for _, dir := range stage.Directories {
					path := filepath.Clean(dir.Path)

					allowed := false
					for _, prefix := range writable {
						if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
							allowed = true
							break
						}
					}

					Expect(allowed).To(BeTrue(),
						"%s asks for %s, which is not under any writable path of an installed node (%s)",
						name, dir.Path, strings.Join(writable, " "))
				}
			}
		}
	})

	// The check above reads the paths; this one runs the plugin that acts on
	// them, against a root whose /usr answers EROFS the way the kernel does.
	It("runs every directories stage without an error on an installed node", func() {
		log := logger.NewNullLogger()

		for name, stages := range shippedStages() {
			for _, stage := range stages {
				if len(stage.Directories) == 0 {
					continue
				}
				Expect(plugins.EnsureDirectories(log, stage, installedRoot(), nil)).
					To(Succeed(), name)
			}
		}
	})
})
