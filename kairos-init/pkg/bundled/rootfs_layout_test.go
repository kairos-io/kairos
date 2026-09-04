package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// persistentStatePaths pulls PERSISTENT_STATE_PATHS out of the active/passive
// rootfs stage of the shipped 00_rootfs.yaml and splits it into single paths.
func persistentStatePaths() []string {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/00_rootfs.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages struct {
			Rootfs []struct {
				Environment struct {
					PersistentStatePaths string `yaml:"PERSISTENT_STATE_PATHS"`
				} `yaml:"environment"`
			} `yaml:"rootfs"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	for _, stage := range config.Stages.Rootfs {
		if stage.Environment.PersistentStatePaths != "" {
			return strings.Fields(stage.Environment.PersistentStatePaths)
		}
	}

	Fail("no rootfs stage declares PERSISTENT_STATE_PATHS")
	return nil
}

var _ = Describe("RootfsLayout", func() {
	It("still declares the persistent state paths", func() {
		Expect(persistentStatePaths()).ToNot(BeEmpty())
	})

	// Every persistent path is rsynced into /usr/local/.state and bind-mounted
	// over the booted slot. A path under /usr is image content, so persisting it
	// carries one slot's binaries into the other: after a rollback the older
	// image ran the newer image's /usr/libexec/sudo helpers against its own
	// older glibc, and sudo stopped working.
	//
	// The prefix is /usr/lib and not /usr because /usr/local is the persistent
	// partition itself, mounted as a volume rather than bind-mounted out of one.
	It("persists no path owned by the image", func() {
		for _, path := range persistentStatePaths() {
			Expect(path).ToNot(HavePrefix("/usr/lib"), "%s is image content, persisting it breaks rollback", path)
		}
	})

	It("does not persist /usr/libexec", func() {
		Expect(persistentStatePaths()).ToNot(ContainElement("/usr/libexec"))
	})

	It("keeps persisting machine-owned state", func() {
		Expect(persistentStatePaths()).To(ContainElements("/home", "/root", "/opt", "/var/log"))
	})
})
