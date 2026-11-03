package bundled_test

import (
	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MissingFips", func() {
	var kairos, provider, installer []byte

	BeforeEach(func() {
		kairos, provider, installer = bundled.EmbeddedKairosFips, bundled.EmbeddedKairosProviderFips, bundled.EmbeddedKairosInstallerFips
	})

	AfterEach(func() {
		bundled.EmbeddedKairosFips, bundled.EmbeddedKairosProviderFips, bundled.EmbeddedKairosInstallerFips = kairos, provider, installer
	})

	It("names every empty FIPS binary (a build with FIPS placeholders)", func() {
		bundled.EmbeddedKairosFips = nil
		bundled.EmbeddedKairosProviderFips = []byte("provider")
		bundled.EmbeddedKairosInstallerFips = []byte{}
		Expect(bundled.MissingFips()).To(ConsistOf("kairos", "kairos-installer"))
	})

	It("is empty when every FIPS binary is embedded", func() {
		bundled.EmbeddedKairosFips = []byte("kairos")
		bundled.EmbeddedKairosProviderFips = []byte("provider")
		bundled.EmbeddedKairosInstallerFips = []byte("installer")
		Expect(bundled.MissingFips()).To(BeEmpty())
	})
})
