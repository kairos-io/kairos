package bus_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/internal/bus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bus", func() {
	Context("LoadProviders", func() {
		var providerDir string

		BeforeEach(func() {
			var err error
			providerDir, err = os.MkdirTemp("", "providers")
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(providerDir, "agent-provider-foo"), []byte("#!/bin/bash\n"), 0777)
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			_ = os.RemoveAll(providerDir)
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
})
