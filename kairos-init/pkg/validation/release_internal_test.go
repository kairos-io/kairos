package validation

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
)

// These specs are in package validation so they can read what validate
// decides from the image's kairos-release without exporting anything. The
// suite's RunSpecs lives in package validation_test; Ginkgo registers both
// into the same suite.

var _ = Describe("what validate reads from the image's kairos-release", func() {
	// standardK3s is what the init stage writes for a standard image built
	// with --provider k3s. Note that KAIROS_SOFTWARE_VERSION holds the
	// provider's version and KAIROS_SOFTWARE_VERSION_PREFIX its name.
	standardK3s := map[string]string{
		"KAIROS_VARIANT":                 "standard",
		"KAIROS_MODEL":                   "rpi4",
		"KAIROS_SOFTWARE_VERSION":        "v1.35.5+k3s1",
		"KAIROS_SOFTWARE_VERSION_PREFIX": "k3s",
	}

	Describe("the variant", func() {
		It("is the one the image records, not the one the flags produced", func() {
			// --provider is local to the root command, so a validate run
			// always leaves config.DefaultConfig.Variant at core. The image
			// has to be able to say otherwise.
			config.DefaultConfig.Variant = config.CoreVariant
			DeferCleanup(func() { config.DefaultConfig.Variant = "" })

			variant, err := imageVariant(standardK3s)
			Expect(err).ToNot(HaveOccurred())
			Expect(variant).To(Equal(config.StandardVariant))
		})

		It("is core when the image says core", func() {
			variant, err := imageVariant(map[string]string{"KAIROS_VARIANT": "core"})
			Expect(err).ToNot(HaveOccurred())
			Expect(variant).To(Equal(config.CoreVariant))
		})

		It("reports a value that is not a variant, and falls back to core", func() {
			variant, err := imageVariant(map[string]string{"KAIROS_VARIANT": "deluxe"})
			Expect(err).To(MatchError(ContainSubstring("KAIROS_VARIANT")))
			Expect(err).To(MatchError(ContainSubstring("deluxe")))
			Expect(variant).To(Equal(config.CoreVariant))
		})

		It("stays quiet about a missing key, which the key check already reports", func() {
			variant, err := imageVariant(map[string]string{})
			Expect(err).ToNot(HaveOccurred())
			Expect(variant).To(Equal(config.CoreVariant))
		})
	})

	Describe("the model", func() {
		It("is the one the image was built for", func() {
			// Same reasoning as the variant: -m is local to the root command,
			// so the default is all a validate run would otherwise see.
			config.DefaultConfig.Model = values.Generic.String()
			DeferCleanup(func() { config.DefaultConfig.Model = "" })

			Expect(imageModel(standardK3s)).To(Equal(values.Rpi4.String()))
		})

		It("is generic when the image records none", func() {
			Expect(imageModel(map[string]string{})).To(Equal(values.Generic.String()))
		})
	})

	Describe("the binaries an image must ship", func() {
		It("includes the standard-variant set for a standard image", func() {
			Expect(expectedBinaries(values.DebianFamily, config.StandardVariant, standardK3s)).
				To(ContainElements("agent-provider-kairos", "kairos", "edgevpn"))
		})

		It("leaves the standard-variant set out of a core image", func() {
			Expect(expectedBinaries(values.DebianFamily, config.CoreVariant, map[string]string{})).
				ToNot(ContainElement("edgevpn"))
		})

		It("asks for the provider binary named by the prefix key", func() {
			Expect(expectedBinaries(values.DebianFamily, config.StandardVariant, standardK3s)).
				To(ContainElement("k3s"))

			k0s := map[string]string{
				"KAIROS_SOFTWARE_VERSION":        "v1.34.0+k0s.0",
				"KAIROS_SOFTWARE_VERSION_PREFIX": "k0s",
			}
			Expect(expectedBinaries(values.DebianFamily, config.StandardVariant, k0s)).
				To(ContainElement("k0s"))
		})

		It("does not look for the provider in the version key, which never holds a name", func() {
			onlyVersion := map[string]string{"KAIROS_SOFTWARE_VERSION": "v1.35.5+k3s1"}
			Expect(expectedBinaries(values.DebianFamily, config.StandardVariant, onlyVersion)).
				ToNot(ContainElement("k3s"))
		})

		It("skips mount.nfs on hadron and asks for it everywhere else", func() {
			Expect(expectedBinaries(values.HadronFamily, config.CoreVariant, map[string]string{})).
				ToNot(ContainElement("mount.nfs"))
			Expect(expectedBinaries(values.AlpineFamily, config.CoreVariant, map[string]string{})).
				To(ContainElement("mount.nfs"))
		})

		It("always asks for the binaries every image ships", func() {
			Expect(expectedBinaries(values.DebianFamily, config.CoreVariant, map[string]string{})).
				To(ContainElements("immucore", "kairos-agent", "sudo", "less", "kcrypt-discovery-challenger"))
		})
	})
})
