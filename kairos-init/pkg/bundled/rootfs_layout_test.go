package bundled_test

import (
	"path"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// declaredPersistentStatePaths is one PERSISTENT_STATE_PATHS declaration: the
// paths it lists and the file and stage they came from, so a failure names the
// line to edit.
type declaredPersistentStatePaths struct {
	file  string
	stage string
	paths []string
}

// persistentStatePathsIn parses one bundled cloud-config and returns every
// PERSISTENT_STATE_PATHS its stages declare, from any stage name. Both
// 00_rootfs.yaml and 01_extra_binds.yaml feed the same immucore list: the
// first through /run/cos/cos-layout.env, the second through
// /run/cos/extra-layout.env, which LoadEnvLayoutDagStep appends to the same
// BindMounts slice. So the rules below have to hold for every file, not only
// for the one that happens to carry the default list.
func persistentStatePathsIn(name string) []declaredPersistentStatePaths {
	raw, err := bundled.EmbeddedConfigs.ReadFile(path.Join("cloudconfigs", name))
	Expect(err).ToNot(HaveOccurred(), name)

	var config struct {
		Stages map[string][]struct {
			Name        string `yaml:"name"`
			Environment struct {
				PersistentStatePaths string `yaml:"PERSISTENT_STATE_PATHS"`
			} `yaml:"environment"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed(), name)

	var declared []declaredPersistentStatePaths
	for _, stages := range config.Stages {
		for _, stage := range stages {
			if stage.Environment.PersistentStatePaths == "" {
				continue
			}
			declared = append(declared, declaredPersistentStatePaths{
				file:  name,
				stage: stage.Name,
				paths: strings.Fields(stage.Environment.PersistentStatePaths),
			})
		}
	}
	return declared
}

// allPersistentStatePaths walks every bundled cloud-config, so a new file that
// declares persistent paths is covered without being added here.
func allPersistentStatePaths() []declaredPersistentStatePaths {
	entries, err := bundled.EmbeddedConfigs.ReadDir("cloudconfigs")
	Expect(err).ToNot(HaveOccurred())

	var declared []declaredPersistentStatePaths
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		declared = append(declared, persistentStatePathsIn(entry.Name())...)
	}
	return declared
}

// persistentStatePaths pulls PERSISTENT_STATE_PATHS out of the active/passive
// rootfs stage of the shipped 00_rootfs.yaml and splits it into single paths.
func persistentStatePaths() []string {
	for _, declared := range persistentStatePathsIn("00_rootfs.yaml") {
		return declared.paths
	}

	Fail("no rootfs stage declares PERSISTENT_STATE_PATHS")
	return nil
}

var _ = Describe("RootfsLayout", func() {
	It("still declares the persistent state paths", func() {
		Expect(persistentStatePaths()).ToNot(BeEmpty())
	})

	It("does not persist /usr/libexec", func() {
		Expect(persistentStatePaths()).ToNot(ContainElement("/usr/libexec"))
	})

	It("keeps persisting machine-owned state", func() {
		Expect(persistentStatePaths()).To(ContainElements("/home", "/root", "/opt", "/var/log"))
	})
})

var _ = Describe("Bundled persistent state paths", func() {
	// Guard against every assertion below passing because the parser stopped
	// finding anything.
	It("finds a declaration in more than one file", func() {
		files := map[string]bool{}
		for _, declared := range allPersistentStatePaths() {
			files[declared.file] = true
		}
		Expect(files).To(HaveKey("00_rootfs.yaml"))
		Expect(files).To(HaveKey("01_extra_binds.yaml"))
	})

	// Every persistent path is rsynced into /usr/local/.state and bind-mounted
	// over the booted slot. A path under /usr is image content, so persisting it
	// carries one slot's content into the other: after a rollback the older
	// image ran the newer image's /usr/libexec/sudo helpers against its own
	// older glibc, and sudo stopped working (kairos-io/kairos#4304). The same
	// applied to /usr/share/pki/trust on SUSE, where it is the distro CA bundle
	// and the stale copy keeps trusting a root the image has distrusted
	// (kairos-io/kairos#4747).
	//
	// /usr/local is exempt because it is the persistent partition itself,
	// mounted as a volume rather than bind-mounted out of one.
	It("persists no path owned by the image", func() {
		for _, declared := range allPersistentStatePaths() {
			for _, p := range declared.paths {
				if p == "/usr/local" || strings.HasPrefix(p, "/usr/local/") {
					continue
				}
				Expect(p).ToNot(HavePrefix("/usr/"),
					"%s: %q persists %s, which is image content, so it breaks rollback",
					declared.file, declared.stage, p)
			}
		}
	})

	// immucore creates a mountpoint the image does not ship
	// (op.MountBind's PrepareCallback), and outside the RW_PATHS overlays the
	// rootfs is read-only, so that mkdir returns EROFS and the bind fails on
	// every boot. That is kairos-io/kairos#3442. A path outside those overlays
	// has to be created at image build time, the way steps_init.go creates
	// /snap, so it needs an entry here as well as there.
	It("persists nothing outside an overlay that the image does not ship", func() {
		// Outside the overlays, the mountpoint has to be there already. Each
		// entry records why it is, because that is the fact a new entry has to
		// establish before it is added.
		presentOnTheRootfs := map[string]string{
			"/home":      "shipped by every base image",
			"/opt":       "shipped by every base image",
			"/root":      "shipped by every base image",
			"/snap":      `created at image build time by the "Create snap dir in rootfs" step of kairos-init/pkg/stages/steps_init.go`,
			"/usr/local": "the persistent partition itself, mounted as a volume by 00_rootfs.yaml",
		}

		// RW_PATHS from 00_rootfs.yaml. A path under one of these is on a
		// tmpfs overlay, so immucore can always create the mountpoint.
		overlays := []string{"/var/", "/etc/", "/srv/"}

		for _, declared := range allPersistentStatePaths() {
			for _, p := range declared.paths {
				underOverlay := false
				for _, overlay := range overlays {
					if strings.HasPrefix(p, overlay) {
						underOverlay = true
					}
				}
				if underOverlay {
					continue
				}
				Expect(presentOnTheRootfs).To(HaveKey(p),
					"%s: %q persists %s, which is neither under an RW_PATHS overlay nor known to be on the rootfs, so immucore's mkdir returns EROFS and the bind fails on every boot",
					declared.file, declared.stage, p)
			}
		}
	})
})
