package bus_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/internal/bus"

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

		It("reports the paths it loaded from", func() {
			b := bus.NewBus()
			Expect(b.LoadedPaths()).To(BeEmpty())

			b.LoadProviders(providerDir)
			Expect(b.LoadedPaths()).To(Equal([]string{providerDir}))
		})
	})

	Context("Initialize", func() {
		var firstDir, secondDir string

		BeforeEach(func() {
			var err error
			firstDir, err = os.MkdirTemp("", "providers-first")
			Expect(err).ToNot(HaveOccurred())

			secondDir, err = os.MkdirTemp("", "providers-second")
			Expect(err).ToNot(HaveOccurred())

			for dir, name := range map[string]string{
				firstDir:  "agent-provider-first",
				secondDir: "agent-provider-second",
			} {
				Expect(os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"), 0777)).To(Succeed())
			}
		})

		AfterEach(func() {
			_ = os.RemoveAll(firstDir)
			_ = os.RemoveAll(secondDir)
		})

		It("loads the paths it is given", func() {
			b := bus.NewBus()
			b.Initialize(firstDir)

			Expect(b.LoadedPaths()).To(Equal([]string{firstDir}))
			Expect(b.HasRegisteredPlugins()).To(BeTrue())
		})

		// Registering subscribes every provider on the bus to every event, so
		// a second registration would make each provider already loaded run
		// twice. The first caller is therefore the one whose paths count, and
		// a later caller asking for different ones has to be told.
		It("keeps the providers of the first call when a later one asks for other paths", func() {
			b := bus.NewBus()
			b.Initialize(firstDir)
			b.Initialize(secondDir)

			Expect(b.LoadedPaths()).To(Equal([]string{firstDir}))

			var names []string
			for _, p := range b.Plugins {
				names = append(names, p.Name)
			}
			Expect(names).To(ContainElement("first"))
			Expect(names).ToNot(ContainElement("second"))
		})
	})
})
