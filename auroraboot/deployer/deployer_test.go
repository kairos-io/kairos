package deployer_test

import (
	"github.com/kairos-io/AuroraBoot/deployer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LoadByte", func() {
	It("reads the allow-insecure-registries key", func() {
		cfg, _, err := deployer.LoadByte([]byte("allow-insecure-registries: true\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeTrue())
	})

	It("still honours the deprecated insecure key", func() {
		cfg, _, err := deployer.LoadByte([]byte("insecure: true\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeTrue())
	})

	It("defaults both flags to false when neither key is present", func() {
		cfg, _, err := deployer.LoadByte([]byte("arch: amd64\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeFalse())
	})

	It("reads raw disk size overrides", func() {
		cfg, _, err := deployer.LoadByte([]byte("disk:\n  size: \"65000\"\n  state_size: \"30000\"\n  recovery_image_size: \"12000\"\n"))
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Disk.Size).To(Equal("65000"))
		Expect(cfg.Disk.StateSize).To(Equal("30000"))
		Expect(cfg.Disk.RecoveryImageSize).To(Equal("12000"))
	})
})
