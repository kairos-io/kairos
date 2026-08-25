package provider_test

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/kairos-io/kairos/v4/sdk/bus"

	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"

	. "github.com/kairos-io/provider-kairos/v2/internal/provider"
	"github.com/mudler/go-pluggable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Challenge provider", func() {
	Context("network token", func() {
		e := &pluggable.Event{}

		BeforeEach(func() {
			e = &pluggable.Event{}
		})

		It("returns it if provided", func() {
			f, err := ioutil.TempFile(os.TempDir(), "tests")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(f.Name())

			cfg := &providerConfig.Config{
				P2P: &providerConfig.P2P{
					NetworkToken: "foo",
				},
			}
			d, err := yaml.Marshal(cfg)
			Expect(err).ToNot(HaveOccurred())

			c := &bus.EventPayload{Config: string(d)}
			dat, err := json.Marshal(c)
			Expect(err).ToNot(HaveOccurred())

			e.Data = string(dat)
			resp := Challenge(e)

			Expect(string(resp.Data)).Should(ContainSubstring("foo"))
		})

		It("generates it if not provided", func() {
			f, err := ioutil.TempFile(os.TempDir(), "tests")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(f.Name())

			cfg := &providerConfig.Config{
				P2P: &providerConfig.P2P{
					NetworkToken: "",
				},
			}
			d, err := yaml.Marshal(cfg)
			Expect(err).ToNot(HaveOccurred())
			c := &bus.EventPayload{Config: string(d)}
			dat, err := json.Marshal(c)
			Expect(err).ToNot(HaveOccurred())

			e.Data = string(dat)
			resp := Challenge(e)

			Expect(len(string(resp.Data))).Should(BeNumerically(">", 12))
		})
	})
})
