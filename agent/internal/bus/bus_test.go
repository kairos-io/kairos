package bus_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/internal/bus"
	sdkbus "github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/mudler/go-pluggable"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeProvider drops an executable provider in dir that answers every event
// with the given JSON response body.
func writeProvider(dir, name, response string) string {
	path := filepath.Join(dir, name)
	script := "#!/bin/bash\ncat > /dev/null\necho '" + response + "'\n"
	Expect(os.WriteFile(path, []byte(script), 0777)).To(Succeed())

	return path
}

var _ = Describe("Bus", func() {
	var providerDir string

	BeforeEach(func() {
		var err error
		providerDir, err = os.MkdirTemp("", "providers")
		Expect(err).ToNot(HaveOccurred())

		DeferCleanup(func() { _ = os.RemoveAll(providerDir) })
	})

	Context("LoadProviders", func() {
		BeforeEach(func() {
			writeProvider(providerDir, "agent-provider-foo", "{}")
		})

		It("loads providers from the given override paths", func() {
			b := bus.NewBus()
			b.LoadProviders(providerDir)

			Expect(b.HasRegisteredPlugins()).To(BeTrue())

			var found bool
			for _, p := range b.Plugins {
				if p.Executable == filepath.Join(providerDir, "agent-provider-foo") {
					found = true
					Expect(p.Name).To(Equal("foo"))
				}
			}
			Expect(found).To(BeTrue(), "expected provider from override path to be loaded")
		})

		It("does not load providers from override paths when none are given", func() {
			b := bus.NewBus()
			b.LoadProviders()

			for _, p := range b.Plugins {
				Expect(p.Executable).ToNot(Equal(filepath.Join(providerDir, "agent-provider-foo")))
			}
		})
	})

	Context("Publish, when a provider fails", func() {
		It("returns the error instead of ending the process", func() {
			writeProvider(providerDir, "agent-provider-broken", `{"error":"the provider is unhappy"}`)

			b := bus.NewBus()
			b.Initialize(providerDir)

			_, err := b.Publish(sdkbus.EventBootstrap, struct{}{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("the provider is unhappy"))
			Expect(err.Error()).To(ContainSubstring("broken"))
		})

		It("still asks the providers that come after it", func() {
			// Autoload sorts by the glob, so "a-broken" is asked before "b-good".
			writeProvider(providerDir, "agent-provider-a-broken", `{"error":"down"}`)
			writeProvider(providerDir, "agent-provider-b-good", `{"state":"reached"}`)

			b := bus.NewBus()
			b.Initialize(providerDir)

			var answered []string
			b.Response(sdkbus.EventBootstrap, func(p *pluggable.Plugin, _ *pluggable.EventResponse) {
				answered = append(answered, p.Name)
			})

			_, err := b.Publish(sdkbus.EventBootstrap, struct{}{})
			Expect(err).To(HaveOccurred())
			Expect(answered).To(ConsistOf("a-broken", "b-good"))
		})

		It("reports nothing once a later Publish succeeds", func() {
			writeProvider(providerDir, "agent-provider-broken", `{"error":"transient"}`)

			b := bus.NewBus()
			b.Initialize(providerDir)

			_, err := b.Publish(sdkbus.EventBootstrap, struct{}{})
			Expect(err).To(HaveOccurred())

			writeProvider(providerDir, "agent-provider-broken", `{"state":"recovered"}`)

			_, err = b.Publish(sdkbus.EventBootstrap, struct{}{})
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
