package bus_test

import (
	"os"
	"path/filepath"
	"sync"

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

		It("still asks the other providers", func() {
			writeProvider(providerDir, "kcrypt-discovery-a-broken", `{"error":"down"}`)
			writeProvider(providerDir, "kcrypt-discovery-b-good", `{"data":"s3cret"}`)

			b := bus.NewBus()
			b.Initialize()

			// Providers answer on their own goroutines, in no fixed order, so
			// the listener has to guard what it records.
			var mu sync.Mutex
			var passwords []string
			b.Response(bus.EventDiscoveryPassword, func(_ *pluggable.Plugin, r *pluggable.EventResponse) {
				if r.Data == "" {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				passwords = append(passwords, r.Data)
			})

			_, err := b.Publish(bus.EventDiscoveryPassword, struct{}{})
			Expect(err).To(HaveOccurred())

			mu.Lock()
			defer mu.Unlock()
			Expect(passwords).To(ConsistOf("s3cret"))
		})

		It("reports every provider that failed, not just the first", func() {
			// Two failures in one Publish. Their responses are handled
			// concurrently, so this is the spec that catches an unguarded
			// append to the error slice.
			writeProvider(providerDir, "kcrypt-discovery-a-broken", `{"error":"the KMS is unreachable"}`)
			writeProvider(providerDir, "kcrypt-discovery-b-broken", `{"error":"the token expired"}`)

			b := bus.NewBus()
			b.Initialize()

			_, err := b.Publish(bus.EventDiscoveryPassword, struct{}{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("the KMS is unreachable"))
			Expect(err.Error()).To(ContainSubstring("the token expired"))
		})
	})
})
