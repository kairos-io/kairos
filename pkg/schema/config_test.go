package schema_test

import (
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ISO extensions", func() {
	It("stores a catalog and parsed requests", func() {
		iso := schema.ISO{
			ExtensionsCatalogs: []string{"catalog.yaml"},
			Extensions:         []extensions.Request{{Name: "foo", Version: "v1"}},
		}
		Expect(iso.ExtensionsCatalogs).To(Equal([]string{"catalog.yaml"}))
		Expect(iso.Extensions).To(Equal([]extensions.Request{{Name: "foo", Version: "v1"}}))
	})

	It("keeps the empty configuration as a no-op", func() {
		iso := schema.ISO{}
		Expect(iso.ExtensionsCatalogs).To(BeEmpty())
		Expect(iso.Extensions).To(BeEmpty())
	})

	It("accepts requests with no catalog, which reads the default one", func() {
		cfg := schema.Config{ISO: schema.ISO{Extensions: []extensions.Request{{Name: "foo"}}}}
		Expect(cfg.Validate()).To(Succeed())
	})
})

var _ = Describe("ISO HandleDeprecations", func() {
	var (
		iso schema.ISO
		log logger.KairosLogger
	)

	BeforeEach(func() {
		iso = schema.ISO{}
		log = logger.NewKairosLogger("test", "error", false)
	})

	It("does nothing when neither iso.data nor iso.overlay_iso are set", func() {
		iso.HandleDeprecations(log)
		Expect(iso.DataPath).To(BeEmpty())
		Expect(iso.OverlayISO).To(BeEmpty())
	})

	It("migrates iso.data to iso.overlay_iso when only iso.data is set", func() {
		iso.DataPath = "/some/path"
		iso.HandleDeprecations(log)
		Expect(iso.OverlayISO).To(Equal("/some/path"))
		Expect(iso.DataPath).To(BeEmpty())
	})

	It("keeps iso.overlay_iso and clears iso.data when both are set", func() {
		iso.DataPath = "/old/path"
		iso.OverlayISO = "/new/path"
		iso.HandleDeprecations(log)
		Expect(iso.OverlayISO).To(Equal("/new/path"))
		Expect(iso.DataPath).To(BeEmpty())
	})

	It("preserves iso.overlay_iso when only iso.overlay_iso is set", func() {
		iso.OverlayISO = "/overlay/path"
		iso.HandleDeprecations(log)
		Expect(iso.OverlayISO).To(Equal("/overlay/path"))
		Expect(iso.DataPath).To(BeEmpty())
	})

	It("does not affect other ISO fields", func() {
		iso.DataPath = "/some/path"
		iso.OverlayRootfs = "/rootfs"
		iso.Name = "test-iso"
		iso.HandleDeprecations(log)
		Expect(iso.OverlayRootfs).To(Equal("/rootfs"))
		Expect(iso.Name).To(Equal("test-iso"))
	})
})

var _ = Describe("Config HandleDeprecations", func() {
	var (
		cfg schema.Config
		log logger.KairosLogger
	)

	BeforeEach(func() {
		cfg = schema.Config{}
		log = logger.NewKairosLogger("test", "error", false)
	})

	It("does nothing when neither key is set", func() {
		cfg.HandleDeprecations(log)
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeFalse())
		Expect(cfg.DeprecatedInsecure).To(BeFalse())
	})

	It("migrates the deprecated insecure key to allow-insecure-registries", func() {
		cfg.DeprecatedInsecure = true
		cfg.HandleDeprecations(log)
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeTrue())
		Expect(cfg.DeprecatedInsecure).To(BeFalse())
	})

	It("keeps allow-insecure-registries when only that key is set", func() {
		t := true
		cfg.AllowInsecureRegistries = &t
		cfg.HandleDeprecations(log)
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeTrue())
		Expect(cfg.DeprecatedInsecure).To(BeFalse())
	})

	It("does not clobber allow-insecure-registries when both keys are set to true", func() {
		t := true
		cfg.AllowInsecureRegistries = &t
		cfg.DeprecatedInsecure = true
		cfg.HandleDeprecations(log)
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeTrue())
		Expect(cfg.DeprecatedInsecure).To(BeFalse())
	})

	It("does not override an explicit false allow-insecure-registries when insecure is true", func() {
		f := false
		cfg.AllowInsecureRegistries = &f
		cfg.DeprecatedInsecure = true
		cfg.HandleDeprecations(log)
		Expect(cfg.AllowInsecureRegistriesBool()).To(BeFalse())
		Expect(cfg.DeprecatedInsecure).To(BeFalse())
	})
})

var _ = Describe("Config Validate", func() {
	var cfg schema.Config

	BeforeEach(func() {
		cfg = schema.Config{}
	})

	It("passes with no disk options set", func() {
		Expect(cfg.Validate()).To(Succeed())
	})

	It("passes for a plain EFI raw disk build", func() {
		cfg.Disk.EFI = true
		Expect(cfg.Validate()).To(Succeed())
	})

	It("passes for partition-image output on its own", func() {
		cfg.Disk.Partitions = true
		Expect(cfg.Validate()).To(Succeed())
	})

	It("passes for partition-image output combined with efi", func() {
		cfg.Disk.Partitions = true
		cfg.Disk.EFI = true
		Expect(cfg.Validate()).To(Succeed())
	})

	It("rejects partition-image output combined with gce", func() {
		cfg.Disk.Partitions = true
		cfg.Disk.GCE = true
		err := cfg.Validate()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("partitions"))
		Expect(err.Error()).To(ContainSubstring("gce"))
	})

	It("rejects partition-image output combined with vhd", func() {
		cfg.Disk.Partitions = true
		cfg.Disk.VHD = true
		err := cfg.Validate()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("partitions"))
		Expect(err.Error()).To(ContainSubstring("vhd"))
	})

	It("passes for a plain BIOS raw disk build", func() {
		cfg.Disk.BIOS = true
		Expect(cfg.Validate()).To(Succeed())
	})

	DescribeTable("rejects bios combined with an EFI-family output",
		func(set func(*schema.Config), option string) {
			cfg.Disk.BIOS = true
			set(&cfg)
			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("disk.bios"))
			Expect(err.Error()).To(ContainSubstring("disk." + option))
		},
		Entry("efi", func(c *schema.Config) { c.Disk.EFI = true }, "efi"),
		Entry("partitions", func(c *schema.Config) { c.Disk.Partitions = true }, "partitions"),
		Entry("gce", func(c *schema.Config) { c.Disk.GCE = true }, "gce"),
		Entry("vhd", func(c *schema.Config) { c.Disk.VHD = true }, "vhd"),
		Entry("maas", func(c *schema.Config) { c.Disk.MAAS = true }, "maas"),
	)

	It("passes for boot-active combined with efi, gce and vhd", func() {
		cfg.Disk.BootActive = true
		cfg.Disk.EFI = true
		cfg.Disk.GCE = true
		cfg.Disk.VHD = true
		Expect(cfg.Validate()).To(Succeed())
	})

	DescribeTable("rejects boot-active combined with unsupported disk types",
		func(set func(*schema.Config), option string) {
			cfg.Disk.BootActive = true
			set(&cfg)
			err := cfg.Validate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boot_active"))
			Expect(err.Error()).To(ContainSubstring(option))
		},
		Entry("bios", func(c *schema.Config) { c.Disk.BIOS = true }, "bios"),
		Entry("partitions", func(c *schema.Config) { c.Disk.Partitions = true }, "partitions"),
		Entry("maas", func(c *schema.Config) { c.Disk.MAAS = true }, "maas"),
		Entry("bundled extensions", func(c *schema.Config) { c.ISO.Extensions = []extensions.Request{{Name: "k9s"}} }, "iso.extensions"),
	)

	DescribeTable("single-image options",
		func(set func(*schema.Config), wantErr string) {
			set(&cfg)
			err := cfg.Validate()
			if wantErr == "" {
				Expect(err).ToNot(HaveOccurred())
				return
			}
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(wantErr))
		},
		Entry("state_slots with boot_active", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.StateSlots = "1" }, ""),
		Entry("no_recovery with a single slot", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.NoRecovery = true; c.Disk.StateSlots = "1" }, ""),
		Entry("no_recovery with default slots", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.NoRecovery = true }, "disk.no_recovery requires disk.state_slots=1"),
		Entry("no_recovery with room for an upgrade", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.NoRecovery = true; c.Disk.StateSlots = "2" }, "disk.no_recovery requires disk.state_slots=1"),
		Entry("state_slots without boot_active", func(c *schema.Config) { c.Disk.StateSlots = "1" }, "disk.state_slots requires disk.boot_active"),
		Entry("no_recovery without boot_active", func(c *schema.Config) { c.Disk.NoRecovery = true }, "disk.no_recovery requires disk.boot_active"),
		Entry("state_slots too high", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.StateSlots = "4" }, "between 1 and 3"),
		Entry("state_slots zero", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.StateSlots = "0" }, "between 1 and 3"),
		Entry("state_slots not a number", func(c *schema.Config) { c.Disk.BootActive = true; c.Disk.StateSlots = "one" }, "between 1 and 3"),
	)
})
