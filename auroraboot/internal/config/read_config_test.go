package config_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/config"
)

var _ = Describe("ReadConfig --set handling", func() {
	It("applies well-typed --set values to the config and release without error", func() {
		c, r, err := config.ReadConfig("", "", []string{
			"arch=arm64",
			"disable_iso=true",
			"disk.gce=true",
			"disk.size=2000",
			"container_image=oci://example/img:tag",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Arch).To(Equal("arm64"))
		Expect(c.DisableISOboot).To(BeTrue())
		Expect(c.Disk.GCE).To(BeTrue())
		Expect(c.Disk.Size).To(Equal("2000"))
		Expect(r.ContainerImage).To(Equal("oci://example/img:tag"))
	})

	It("parses the boot-active single-image options from --set", func() {
		c, _, err := config.ReadConfig("", "", []string{
			"disk.raw=true",
			"disk.boot_active=true",
			"disk.state_slots=1",
			"disk.no_recovery=true",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.Disk.EFI).To(BeTrue())
		Expect(c.Disk.BootActive).To(BeTrue())
		Expect(c.Disk.StateSlots).To(Equal("1"))
		Expect(c.Disk.NoRecovery).To(BeTrue())
	})

	It("surfaces an error for a --set value that does not fit its field's type", func() {
		// A non-boolean into a boolean field used to be silently dropped (the
		// field kept its zero value, no error reported). It must now surface, so a
		// typo in --set is not swallowed.
		_, _, err := config.ReadConfig("", "", []string{"disable_iso=notabool"})
		// disable_iso is a Config field, so the mismatch surfaces from the config
		// target specifically — assert that exact wrapped error, not just "some
		// error occurred".
		Expect(err).To(MatchError(ContainSubstring("applying --set values to config")))
	})

	It("surfaces an error for a --set value that does not fit a release-artifact field's type", func() {
		// container_image is a ReleaseArtifact string field and not a Config field,
		// so the config target ignores it (unknown key) and the mismatch surfaces
		// from the release-artifact target. A nested map cannot fit a string.
		_, _, err := config.ReadConfig("", "", []string{"container_image.nested=x"})
		Expect(err).To(MatchError(ContainSubstring("applying --set values to release artifact")))
	})

	It("honours the deprecated insecure key from --set", func() {
		c, _, err := config.ReadConfig("", "", []string{"insecure=true"})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.AllowInsecureRegistriesBool()).To(BeTrue())
		Expect(c.DeprecatedInsecure).To(BeFalse())
	})

	It("reads allow-insecure-registries from --set", func() {
		c, _, err := config.ReadConfig("", "", []string{"allow-insecure-registries=true"})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.AllowInsecureRegistriesBool()).To(BeTrue())
	})

	It("does not let --set insecure=true override an explicit allow-insecure-registries=false", func() {
		c, _, err := config.ReadConfig("", "", []string{"insecure=true", "allow-insecure-registries=false"})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.AllowInsecureRegistries).NotTo(BeNil())
		Expect(c.AllowInsecureRegistriesBool()).To(BeFalse())
	})

	It("does not let --set insecure=true override allow-insecure-registries: false from the config file", func() {
		f := filepath.Join(GinkgoT().TempDir(), "config.yaml")
		Expect(os.WriteFile(f, []byte("allow-insecure-registries: false\n"), 0o600)).To(Succeed())
		c, _, err := config.ReadConfig(f, "", []string{"insecure=true"})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.AllowInsecureRegistries).NotTo(BeNil())
		Expect(c.AllowInsecureRegistriesBool()).To(BeFalse())
	})

	It("accepts an empty option set", func() {
		c, r, err := config.ReadConfig("", "", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(c).NotTo(BeNil())
		Expect(r).NotTo(BeNil())
	})
})

var _ = Describe("ReadCloudConfig read errors", func() {
	It("returns the read error for a path that exists but cannot be read", func() {
		// A directory passes os.Stat and fails os.ReadFile, the same way an
		// unreadable file does, and it also fails when the tests run as root.
		_, err := config.ReadCloudConfig(GinkgoT().TempDir(), nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).NotTo(ContainSubstring("contents are empty"))
		Expect(err.Error()).To(ContainSubstring("is a directory"))
	})
})
