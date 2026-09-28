package bundled_test

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// These stages each write to the disk on every boot, which cannot work when the
// media is write-protected. immucore says so by writing the sentinel; the guards
// below are what makes the stages notice. The sentinel path is spelled out in
// YAML because YAML cannot reference a Go constant, so these specs are what stop
// the two from drifting. Each file is parsed and the guard is asserted on the
// named stage, not found anywhere in the file: a guard on the wrong stage would
// otherwise pass.
var _ = Describe("read-only media guards in the shipped cloud-configs", func() {
	sentinel := filepath.Join(sdkConstants.SentinelDir, sdkConstants.WriteProtectedSentinelName)
	guard := fmt.Sprintf("[ ! -f %s ]", sentinel)

	type stage struct {
		Name string `yaml:"name"`
		If   string `yaml:"if"`
	}
	type config struct {
		Stages map[string][]stage `yaml:"stages"`
	}

	load := func(file string) config {
		raw, err := bundled.EmbeddedConfigs.ReadFile(filepath.Join("cloudconfigs", file))
		Expect(err).ToNot(HaveOccurred())
		var c config
		// yaml.v3 honours the << merge key, so anchored stages reused in other
		// phases come back with their if: in place.
		Expect(yaml.Unmarshal(raw, &c)).To(Succeed())
		return c
	}

	find := func(c config, phase, name string) stage {
		for _, st := range c.Stages[phase] {
			if st.Name == name {
				return st
			}
		}
		Fail(fmt.Sprintf("no stage named %q in phase %q; it was renamed or removed, so this guard needs revisiting", name, phase))
		return stage{}
	}

	DescribeTable("the stage is skipped on write-protected media",
		func(file, phase, name string) {
			st := find(load(file), phase, name)
			Expect(strings.Contains(st.If, guard)).To(BeTrue(),
				"%s stage %q in phase %s writes to the disk on every boot and its condition %q has no read-only guard", file, name, phase, st.If)
		},
		// yip's layout plugin opens the whole disk read-write in both
		// VerifyAndRepairHeaders and ExpandLastPartition.
		Entry("growing the persistent partition", "00_rootfs.yaml", "rootfs.after", "Grow persistent"),
		// Remounts COS_STATE read-write and rewrites the grub environment.
		Entry("clearing the GRUB sentinels", "08_grub.yaml", "boot.before", "Remove GRUB sentinels"),
		// These write to /oem, which is mounted read-only. The anchored copies
		// in the later phases inherit the guard through the merge key.
		Entry("moving legacy userdata, rootfs.before", "00_datasource.yaml", "rootfs.before", "Move old datasource to new location"),
		Entry("moving legacy userdata, initramfs.before", "00_datasource.yaml", "initramfs.before", "Move old datasource to new location"),
		Entry("removing legacy userdata, rootfs.before", "00_datasource.yaml", "rootfs.before", "Remove old userdata"),
		Entry("removing legacy userdata, initramfs.before", "00_datasource.yaml", "initramfs.before", "Remove old userdata"),
		Entry("pulling userdata, rootfs.before", "00_datasource.yaml", "rootfs.before", "Pull data from provider"),
		Entry("pulling userdata, initramfs.before", "00_datasource.yaml", "initramfs.before", "Pull data from provider"),
		// chown -R root:admin /oem on every boot.
		Entry("fixing /oem permissions", "10_accounting.yaml", "initramfs", "Ensure runtime permission"),
	)

	It("does not guard a stage that has no business being guarded", func() {
		// The upgrade-failure sentinel is written to /run, not to the disk, and
		// must keep firing on write-protected media.
		st := find(load("08_grub.yaml"), "boot.before", "Create upgrade failure sentinel if necessary")
		Expect(st.If).ToNot(ContainSubstring(sentinel))
	})
})
