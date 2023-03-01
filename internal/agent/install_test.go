package agent

import (
	"context"
	"github.com/kairos-io/kairos/pkg/config"
	"gopkg.in/yaml.v3"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("prepareConfiguration", func() {
	path := "/foo/bar"
	url := "https://example.com"
	ctx, cancel := context.WithCancel(context.Background())

	It("returns a file path with no modifications", func() {
		source, err := prepareConfiguration(ctx, path)

		Expect(err).ToNot(HaveOccurred())
		Expect(source).To(Equal(path))
	})

	It("creates a configuration file containing the given url", func() {
		source, err := prepareConfiguration(ctx, url)

		Expect(err).ToNot(HaveOccurred())
		Expect(source).ToNot(Equal(path))

		f, err := os.Open(source)
		Expect(err).ToNot(HaveOccurred())

		var cfg config.Config
		err = yaml.NewDecoder(f).Decode(&cfg)
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.ConfigURL).To(Equal(url))
	})

	It("cleans up the configuration file after context is done", func() {
		source, err := prepareConfiguration(ctx, url)
		cancel()

		_, err = os.Stat(source)
		Expect(os.IsNotExist(err))
	})
})

func TestPrepareConfiguration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "prepareConfiguration Suite")
}
