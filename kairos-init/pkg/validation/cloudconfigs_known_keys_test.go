package validation_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const cloudConfigsDir = "../bundled/cloudconfigs"

// yip unmarshals a stage without KnownFields, so a key it does not know is not
// an error, it is a no-op. A misspelled key inside a files:, directories: or
// downloads: entry therefore leaves that field at its zero value and yip
// applies the zero value: `permsisions: 0600` on /etc/sudoers left
// File.Permissions at 0, and writeFile chmods unconditionally, so the file was
// created and then set to 0000 on every boot (kairos-io/kairos#4698).
//
// Nothing about that fails loudly, so pin it here: decode every bundled
// cloud-config against yip's own schema with strict field checking, and name
// the offending key when it does not fit.
var _ = Describe("Bundled cloudconfigs schema keys", func() {
	It("uses only keys yip's schema declares", func() {
		entries, err := os.ReadDir(cloudConfigsDir)
		Expect(err).NotTo(HaveOccurred(), "read %s", cloudConfigsDir)

		checked := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
				continue
			}
			path := filepath.Join(cloudConfigsDir, entry.Name())
			content, err := os.ReadFile(path)
			Expect(err).NotTo(HaveOccurred(), "read %s", path)

			decoder := yaml.NewDecoder(bytes.NewReader(content))
			decoder.KnownFields(true)

			var config schema.YipConfig
			Expect(decoder.Decode(&config)).To(Succeed(),
				"%s: yip would silently ignore this key, leaving the field it names at its zero value", entry.Name())
			checked++
		}

		// Guard against the loop finding nothing and passing vacuously.
		Expect(checked).To(BeNumerically(">=", 30), "expected the bundled cloudconfigs to be present")
	})
})

// The strict decode above catches a key yip does not know. It cannot catch a
// key that is simply absent: an omitted `permissions` leaves File.Permissions
// at 0 and writeFile chmods to 0000 just the same. Pin the two files that
// carried the typo so a later edit that drops the key is caught too.
var _ = Describe("Bundled cloudconfigs file modes", func() {
	DescribeTable("declares the mode it means to apply",
		func(cloudConfig, stage, path string, mode uint32) {
			content, err := os.ReadFile(filepath.Join(cloudConfigsDir, cloudConfig))
			Expect(err).NotTo(HaveOccurred(), "read %s", cloudConfig)

			var config schema.YipConfig
			Expect(yaml.Unmarshal(content, &config)).To(Succeed(), "parse %s", cloudConfig)

			found := false
			for _, step := range config.Stages[stage] {
				for _, file := range step.Files {
					if file.Path != path {
						continue
					}
					found = true
					Expect(file.Permissions).To(Equal(mode),
						"%s: stage %q writes %s with mode %#o", cloudConfig, stage, path, file.Permissions)
				}
			}
			Expect(found).To(BeTrue(), "%s: stage %q writes no %s", cloudConfig, stage, path)
		},
		Entry("sudoers", "10_accounting.yaml", "initramfs", "/etc/sudoers", uint32(0o600)),
		Entry("grub boot assessment", "08_grub.yaml", "after-install", "/tmp/mnt/STATE/grub_boot_assessment", uint32(0o600)),
		Entry("grub boot assessment, after reset", "08_grub.yaml", "after-reset", "/tmp/mnt/STATE/grub_boot_assessment", uint32(0o600)),
		Entry("grub boot assessment, after upgrade", "08_grub.yaml", "after-upgrade", "/tmp/mnt/STATE/grub_boot_assessment", uint32(0o600)),
	)
})
