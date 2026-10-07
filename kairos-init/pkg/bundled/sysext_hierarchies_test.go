package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// collectStrings walks a decoded cloud-config and returns every scalar string
// in it, so that a value buried in a file's content: block is reachable
// without modelling every stage shape in 99_sysext.yaml.
func collectStrings(node any, out *[]string) {
	switch v := node.(type) {
	case string:
		*out = append(*out, v)
	case []any:
		for _, item := range v {
			collectStrings(item, out)
		}
	case map[string]any:
		for _, item := range v {
			collectStrings(item, out)
		}
	}
}

// sysextHierarchies returns one entry per SYSTEMD_SYSEXT_HIERARCHIES
// assignment the shipped 99_sysext.yaml makes, already split into paths.
// There are three: the UKI drop-in, the GRUB drop-in and the profile.d
// snippet, and they are expected to agree.
func sysextHierarchies() [][]string {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/99_sysext.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config map[string]any
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	var scalars []string
	collectStrings(config, &scalars)

	var found [][]string
	for _, scalar := range scalars {
		for _, line := range strings.Split(scalar, "\n") {
			_, value, ok := strings.Cut(line, "SYSTEMD_SYSEXT_HIERARCHIES=")
			if !ok {
				continue
			}
			// The drop-ins write Environment="NAME=a:b", the profile.d
			// snippet writes export NAME="a:b". Both leave a trailing
			// quote once the name is cut off.
			value = strings.Trim(strings.TrimSpace(value), `"`)
			found = append(found, strings.Split(value, ":"))
		}
	}
	return found
}

var _ = Describe("SysextHierarchies", func() {
	It("declares the hierarchies in every place that needs them", func() {
		Expect(sysextHierarchies()).To(HaveLen(3))
	})

	// A merge makes every hierarchy it covers read-only: systemd's
	// merge_hierarchy() mounts the overlay and then calls
	// bind_remount_recursive(..., MS_RDONLY, MS_RDONLY, ...). /usr/local is
	// where COS_PERSISTENT is mounted, so naming any path under it here
	// turns part of the writable partition read-only for the rest of the
	// boot. That broke bundles installing into /usr/local/bin and the
	// cloud-config stages that write scripts there.
	It("merges nothing under the persistent partition", func() {
		for _, hierarchies := range sysextHierarchies() {
			for _, path := range hierarchies {
				Expect(path).ToNot(Equal("/usr/local"),
					"/usr/local is the persistent mount, merging over it makes it read-only")
				Expect(path).ToNot(HavePrefix("/usr/local/"),
					"%s is on the persistent mount, merging over it makes it read-only", path)
			}
		}
	})

	It("still merges the image-owned hierarchies", func() {
		for _, hierarchies := range sysextHierarchies() {
			Expect(hierarchies).To(ContainElements(
				"/usr/bin", "/usr/sbin", "/usr/lib", "/usr/share", "/usr/include", "/usr/src"))
		}
	})

	It("spells the same list everywhere", func() {
		hierarchies := sysextHierarchies()
		for _, other := range hierarchies[1:] {
			Expect(other).To(Equal(hierarchies[0]))
		}
	})
})
