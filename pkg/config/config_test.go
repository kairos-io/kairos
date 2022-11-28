// Copyright © 2022 Ettore Di Giacinto <mudler@c3os.io>
//
// This program is free software; you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation; either version 2 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program; if not, see <http://www.gnu.org/licenses/>.

package config_test

import (
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type TConfig struct {
	Kairos struct {
		NetworkToken string `yaml:"network_token"`
	} `yaml:"kairos"`
}

var _ = Describe("Config", func() {
	var d string
	BeforeEach(func() {
		d, _ = os.MkdirTemp("", "xxxx")
	})

	AfterEach(func() {
		if d != "" {
			os.RemoveAll(d)
		}
	})

	Context("directory", func() {
		headerCheck := func(c *Config) {
			ok, header := HasHeader(c.String(), DefaultHeader)
			ExpectWithOffset(1, ok).To(BeTrue())
			ExpectWithOffset(1, header).To(Equal(DefaultHeader))
		}

		It("reads from bootargs", func() {
			err := os.WriteFile(filepath.Join(d, "b"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(MergeBootLine, WithBootCMDLineFile(filepath.Join(d, "b")))
			Expect(err).ToNot(HaveOccurred())
			headerCheck(c)
			Expect(c.Options["foo"]).To(Equal("bar"))
		})

		It("reads multiple config files", func() {
			var cc string = `#kairos-config
baz: bar
kairos:
  network_token: foo
`
			var c2 string = `
b: f
c: d
`

			err := os.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(d, "b"), []byte(c2), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			providerCfg := &TConfig{}
			err = c.Unmarshal(providerCfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerCfg.Kairos).ToNot(BeNil())
			Expect(providerCfg.Kairos.NetworkToken).To(Equal("foo"))
			all := map[string]string{}
			yaml.Unmarshal([]byte(c.String()), &all)
			Expect(all["b"]).To(Equal("f"))
			Expect(all["baz"]).To(Equal("bar"))
		})

		It("reads config file greedly", func() {

			var cc string = `#kairos-config
baz: bar
kairos:
  network_token: foo
`

			err := os.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = os.WriteFile(filepath.Join(d, "b"), []byte(`
fooz:
			`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			providerCfg := &TConfig{}
			err = c.Unmarshal(providerCfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerCfg.Kairos).ToNot(BeNil())
			Expect(providerCfg.Kairos.NetworkToken).To(Equal("foo"))
			Expect(c.String()).To(Equal(cc))
		})

		It("merges with bootargs", func() {

			var cc string = `#kairos-config
kairos:
  network_token: "foo"

bb: 
  nothing: "foo"
`

			err := os.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = os.WriteFile(filepath.Join(d, "b"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(Directories(d), MergeBootLine, WithBootCMDLineFile(filepath.Join(d, "b")))
			Expect(err).ToNot(HaveOccurred())
			Expect(c.Options["foo"]).To(Equal("bar"))

			providerCfg := &TConfig{}
			err = c.Unmarshal(providerCfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerCfg.Kairos).ToNot(BeNil())
			Expect(providerCfg.Kairos.NetworkToken).To(Equal("foo"))
			_, exists := c.Data()["zz"]
			Expect(exists).To(BeFalse())
		})

		It("reads config file from url", func() {

			var cc string = `
config_url: "https://gist.githubusercontent.com/mudler/ab26e8dd65c69c32ab292685741ca09c/raw/bafae390eae4e6382fb1b68293568696823b3103/test.yaml"
`

			err := os.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			Expect(len(c.Bundles)).To(Equal(1))
			Expect(c.Bundles[0].Targets[0]).To(Equal("package:utils/edgevpn"))
			Expect(c.String()).ToNot(Equal(cc))
		})

		It("keeps header", func() {

			var cc string = `
config_url: "https://gist.githubusercontent.com/mudler/7e3d0426fce8bfaaeb2644f83a9bfe0c/raw/77ded58aab3ee2a8d4117db95e078f81fd08dfde/testgist.yaml"
`

			err := os.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			Expect(len(c.Bundles)).To(Equal(1))
			Expect(c.Bundles[0].Targets[0]).To(Equal("package:utils/edgevpn"))
			Expect(c.String()).ToNot(Equal(cc))

			headerCheck(c)
		})
	})

	Describe("FindYAMLWithKey", func() {
		var c1Path, c2Path string

		BeforeEach(func() {
			var c1 = `
a: 1
b:
  c: foo
d:
  e: bar
`

			var c2 = `
b:
  c: foo2
`
			c1Path = filepath.Join(d, "c1.yaml")
			c2Path = filepath.Join(d, "c2.yaml")

			err := os.WriteFile(c1Path, []byte(c1), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = os.WriteFile(c2Path, []byte(c2), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
		})

		FIt("can find a top level key", func() {
			r, err := FindYAMLWithKey("a", Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(Equal([]string{c1Path}))
		})

		It("can find a nested key", func() {
			r, err := FindYAMLWithKey("d.e", Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(Equal([]string{c1Path}))
		})

		It("returns multiple files when key exists in them", func() {
			r, err := FindYAMLWithKey("b.c", Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(ContainElements(c1Path, c2Path))
		})

		It("return an empty list when key is not found", func() {
			r, err := FindYAMLWithKey("does.not.exist", Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeEmpty())
		})
	})
})
