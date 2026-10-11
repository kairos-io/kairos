package provider

import (
	"github.com/kairos-io/kairos/v4/sdk/bus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Picking the provider the info event reports", func() {
	k3s := bus.ProviderInstalledVersionPayload{Provider: K3s, Version: "k3s version v1.31.1+k3s1"}
	k0s := bus.ProviderInstalledVersionPayload{Provider: K0s, Version: "v1.31.1+k0s.0"}

	It("reports nothing when no provider is installed", func() {
		_, found := pickInstalledProvider(nil)
		Expect(found).To(BeFalse())
	})

	It("reports the only installed provider", func() {
		picked, found := pickInstalledProvider([]bus.ProviderInstalledVersionPayload{k3s})
		Expect(found).To(BeTrue())
		Expect(picked).To(Equal(k3s))
	})

	It("keeps a readable version instead of a later unreadable one", func() {
		// installedProviders probes k3s before k0s. Letting the k0s result
		// overwrite the k3s one leaves an empty version, and kairos-init drops
		// a response with an empty version, so the image would ship with no
		// KAIROS_SOFTWARE_VERSION even though the k3s version was read.
		broken := bus.ProviderInstalledVersionPayload{Provider: K0s, Version: ""}
		picked, found := pickInstalledProvider([]bus.ProviderInstalledVersionPayload{k3s, broken})
		Expect(found).To(BeTrue())
		Expect(picked).To(Equal(k3s))
	})

	It("takes a readable version from a later candidate", func() {
		broken := bus.ProviderInstalledVersionPayload{Provider: K3s, Version: ""}
		picked, found := pickInstalledProvider([]bus.ProviderInstalledVersionPayload{broken, k0s})
		Expect(found).To(BeTrue())
		Expect(picked).To(Equal(k0s))
	})

	It("keeps the input order when both versions read", func() {
		picked, found := pickInstalledProvider([]bus.ProviderInstalledVersionPayload{k3s, k0s})
		Expect(found).To(BeTrue())
		Expect(picked).To(Equal(k3s))
	})

	It("still names a provider when no version could be read", func() {
		// The caller drops this response, but reporting the provider keeps the
		// not-applicable state for images that really have no provider at all.
		noVersion := bus.ProviderInstalledVersionPayload{Provider: K0s, Version: ""}
		picked, found := pickInstalledProvider([]bus.ProviderInstalledVersionPayload{noVersion})
		Expect(found).To(BeTrue())
		Expect(picked.Provider).To(Equal(K0s))
		Expect(picked.Version).To(BeEmpty())
	})
})
