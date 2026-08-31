package cli_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/kairos-io/provider-kairos/v2/internal/cli/token"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type TConfig struct {
	Kairos struct {
		NetworkToken string `yaml:"network_token"`
		P2P          string `yaml:"p2p"`
	} `yaml:"p2p"`
}

var _ = Describe("Get config", func() {
	Context("directory", func() {

		It("replace token in config files", func() {

			var cc string = `#node-config
p2p:
  network_token: "foo"

bb:
  nothing: "foo"
`
			d, _ := os.MkdirTemp("", "xxxx")
			defer os.RemoveAll(d)

			err := os.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = os.WriteFile(filepath.Join(d, "b"), []byte(`
fooz: "bar"
			`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = ReplaceToken([]string{d, "/doesnotexist"}, "baz")
			Expect(err).ToNot(HaveOccurred())

			content, err := os.ReadFile(filepath.Join(d, "test"))
			Expect(err).ToNot(HaveOccurred())

			res := map[interface{}]interface{}{}
			err = yaml.Unmarshal(content, &res)
			Expect(err).ToNot(HaveOccurred())

			// Check by element as they can be unordered
			Expect(res["p2p"]).To(Equal(map[string]interface{}{"network_token": "baz"}))
			Expect(res["bb"]).To(Equal(map[string]interface{}{"nothing": "foo"}))

			Expect(strings.HasPrefix(string(content), "#node-config")).To(BeTrue(), string(content))
		})

		It("preserves comments and key order on token rotation", func() {
			var cc string = `#cloud-config
# Top-level comment explaining the config

p2p:
  network_token: "foo" # the token to be rotated
  auto:
    enable: true

# Comment between top-level sections
bb:
  zzz: "first"
  aaa: "last"
`
			d, _ := os.MkdirTemp("", "preserve")
			defer os.RemoveAll(d)

			err := os.WriteFile(filepath.Join(d, "test.yaml"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = ReplaceToken([]string{d}, "baz")
			Expect(err).ToNot(HaveOccurred())

			out, err := os.ReadFile(filepath.Join(d, "test.yaml"))
			Expect(err).ToNot(HaveOccurred())
			s := string(out)

			Expect(s).To(ContainSubstring(`network_token: "baz"`), s)
			Expect(s).To(ContainSubstring("Top-level comment explaining the config"), s)
			Expect(s).To(ContainSubstring("the token to be rotated"), s)
			Expect(s).To(ContainSubstring("Comment between top-level sections"), s)
			Expect(strings.Index(s, "zzz")).To(BeNumerically("<", strings.Index(s, "aaa")), s)
		})
	})
})
