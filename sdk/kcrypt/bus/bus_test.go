package bus_test

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/kcrypt/bus"
	"github.com/mudler/go-pluggable"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeProvider drops an executable discovery provider in dir that answers
// every event with the given JSON response body.
func writeProvider(dir, name, response string) {
	script := "#!/bin/bash\ncat > /dev/null\necho '" + response + "'\n"
	Expect(os.WriteFile(filepath.Join(dir, name), []byte(script), 0777)).To(Succeed())
}

var _ = Describe("Bus", func() {
	var providerDir string

	// LoadProviders looks in the fixed system paths plus the working directory,
	// so the working directory is the only seam a test has.
	BeforeEach(func() {
		var err error
		providerDir, err = os.MkdirTemp("", "discovery")
		Expect(err).ToNot(HaveOccurred())

		previous, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		Expect(os.Chdir(providerDir)).To(Succeed())

		DeferCleanup(func() {
			Expect(os.Chdir(previous)).To(Succeed())
			_ = os.RemoveAll(providerDir)
		})
	})

	Context("Publish, when a discovery provider fails", func() {
		It("returns the error instead of ending the process", func() {
			writeProvider(providerDir, "kcrypt-discovery-broken", `{"error":"no answer from the KMS"}`)

			b := bus.NewBus()
			b.Initialize()

			_, err := b.Publish(bus.EventDiscoveryPassword, struct{}{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no answer from the KMS"))
			Expect(err.Error()).To(ContainSubstring("broken"))
		})

		It("still asks the providers that come after it", func() {
			// Autoload sorts by the glob, so "a-broken" is asked before "b-good".
			writeProvider(providerDir, "kcrypt-discovery-a-broken", `{"error":"down"}`)
			writeProvider(providerDir, "kcrypt-discovery-b-good", `{"data":"s3cret"}`)

			b := bus.NewBus()
			b.Initialize()

			var passwords []string
			b.Response(bus.EventDiscoveryPassword, func(_ *pluggable.Plugin, r *pluggable.EventResponse) {
				if r.Data != "" {
					passwords = append(passwords, r.Data)
				}
			})

			_, err := b.Publish(bus.EventDiscoveryPassword, struct{}{})
			Expect(err).To(HaveOccurred())
			Expect(passwords).To(ConsistOf("s3cret"))
		})
	})
})
