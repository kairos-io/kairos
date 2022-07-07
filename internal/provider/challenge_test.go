package provider_test

import (
	"encoding/json"
	"io/ioutil"
	"os"

	. "github.com/c3os-io/c3os/internal/provider"
	"github.com/c3os-io/c3os/pkg/bus"
	"github.com/c3os-io/c3os/pkg/config"
	"github.com/mudler/go-pluggable"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

			cfg := &config.Config{
				C3OS: &config.C3OS{
					NetworkToken: "foo",
				},
			}
			c := &bus.EventPayload{Config: cfg.String()}
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

			cfg := &config.Config{
				C3OS: &config.C3OS{
					NetworkToken: "",
				},
			}
			c := &bus.EventPayload{Config: cfg.String()}
			dat, err := json.Marshal(c)
			Expect(err).ToNot(HaveOccurred())

			e.Data = string(dat)
			resp := Challenge(e)

			Expect(len(string(resp.Data))).Should(BeNumerically(">", 12))
		})
	})
})
