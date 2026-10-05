package bus_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/sdk/bus"
)

var _ = Describe("HasProviders", func() {
	var dir string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
	})

	It("finds a provider by the prefix the agent autoloads", func() {
		Expect(os.WriteFile(filepath.Join(dir, bus.DefaultProviderPrefix+"-kairos"), []byte("#!/bin/sh\n"), 0755)).To(Succeed())
		Expect(bus.HasProviders(dir)).To(BeTrue())
	})

	It("reports none when the directory holds no provider", func() {
		Expect(os.WriteFile(filepath.Join(dir, "kairos-agent"), []byte("#!/bin/sh\n"), 0755)).To(Succeed())
		Expect(bus.HasProviders(dir)).To(BeFalse())
	})

	It("reports none for a directory that does not exist", func() {
		Expect(bus.HasProviders(filepath.Join(dir, "absent"))).To(BeFalse())
	})
})
