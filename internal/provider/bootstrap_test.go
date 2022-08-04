package provider_test

import (
	"encoding/json"
	"github.com/c3os-io/c3os/sdk/bus"
	"io/ioutil"
	"os"

	. "github.com/c3os-io/c3os/internal/provider"
	providerConfig "github.com/c3os-io/c3os/internal/provider/config"
	"github.com/mudler/go-pluggable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("Bootstrap provider", func() {
	Context("logging", func() {
		e := &pluggable.Event{}

		BeforeEach(func() {
			e = &pluggable.Event{}
		})

		It("logs to file", func() {
			f, err := ioutil.TempFile(os.TempDir(), "tests")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(f.Name())

			cfg := &providerConfig.Config{
				C3OS: &providerConfig.C3OS{
					NetworkToken: "foo",
				},
			}
			dat, err := yaml.Marshal(cfg)
			Expect(err).ToNot(HaveOccurred())
			payload := &bus.BootstrapPayload{Logfile: f.Name(), Config: string(dat)}

			dat, err = json.Marshal(payload)
			Expect(err).ToNot(HaveOccurred())

			e.Data = string(dat)
			resp := Bootstrap(e)
			dat, _ = json.Marshal(resp)
			Expect(resp.Errored()).To(BeTrue(), string(dat))

			data, err := ioutil.ReadFile(f.Name())
			Expect(err).ToNot(HaveOccurred())

			Expect(string(data)).Should(ContainSubstring("Configuring VPN"))
		})
	})
})
