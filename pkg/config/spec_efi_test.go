package config_test

import (
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/spec"
	sdkConstants "github.com/kairos-io/kairos-sdk/constants"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
)

var _ = Describe("NewInstallElementalPartitions EFI sizing", Label("types", "config", "partitions"), func() {
	logger := sdkLogger.NewKairosLogger("test", "error", false)

	newSpec := func(efi *sdkPartitions.Partition) *v1.InstallSpec {
		return &v1.InstallSpec{
			Active:     sdkImages.Image{Size: 100},
			Passive:    sdkImages.Image{Size: 100},
			Recovery:   sdkImages.Image{Size: 100},
			Partitions: sdkPartitions.ElementalPartitions{EFI: efi},
		}
	}

	It("carries over a configured EFI size so SetFirmwarePartitions preserves it", func() {
		pt := config.NewInstallElementalPartitions(logger, newSpec(&sdkPartitions.Partition{Size: 128}))
		Expect(pt.EFI).ToNot(BeNil())
		Expect(pt.EFI.Size).To(Equal(uint(128)))
	})

	It("leaves EFI unset when not configured, so the firmware default applies later", func() {
		pt := config.NewInstallElementalPartitions(logger, newSpec(nil))
		Expect(pt.EFI).To(BeNil())
	})

	It("leaves EFI unset when the configured size is zero", func() {
		pt := config.NewInstallElementalPartitions(logger, newSpec(&sdkPartitions.Partition{Size: 0}))
		Expect(pt.EFI).To(BeNil())
	})

	// Lock in the config binding the whole feature depends on: EFI is excluded
	// from yaml/json serialization, so this proves install.partitions.efi.size
	// still reaches Partitions.EFI.Size through the viper decode that
	// unmarshallFullSpec uses in production.
	It("decodes install.partitions.efi.size via the production viper path", func() {
		cc := "install:\n  partitions:\n    efi:\n      size: 128\n"
		v := viper.New()
		v.SetConfigType("yaml")
		Expect(v.ReadConfig(strings.NewReader(cc))).To(Succeed())
		sp := &v1.InstallSpec{}
		Expect(v.Sub("install").Unmarshal(sp)).To(Succeed())
		Expect(sp.Partitions.EFI).ToNot(BeNil())
		Expect(sp.Partitions.EFI.Size).To(Equal(uint(128)))
	})

	// End-to-end across the repo boundary: the carry-over here plus the
	// kairos-sdk SetFirmwarePartitions preserve change must together keep a
	// configured EFI size all the way to the finalized partition. This only
	// holds once the sdk pin includes the preserve change.
	It("keeps the configured EFI size through NewInstallElementalPartitions + SetFirmwarePartitions", func() {
		pt := config.NewInstallElementalPartitions(logger, newSpec(&sdkPartitions.Partition{Size: 128}))
		Expect(pt.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(Succeed())
		Expect(pt.EFI).ToNot(BeNil())
		Expect(pt.EFI.Size).To(Equal(uint(128)))
		Expect(pt.EFI.FilesystemLabel).To(Equal(sdkConstants.EfiLabel))
	})

	It("falls back to the default EFI size when none is configured", func() {
		pt := config.NewInstallElementalPartitions(logger, newSpec(nil))
		Expect(pt.SetFirmwarePartitions(sdkConstants.EFI, sdkConstants.GPT)).To(Succeed())
		Expect(pt.EFI).ToNot(BeNil())
		Expect(pt.EFI.Size).To(Equal(sdkConstants.EfiSize))
	})
})
