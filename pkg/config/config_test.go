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
	"fmt"
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/pkg/config"
	"github.com/kairos-io/kairos/pkg/config/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

type TConfig struct {
	Kairos struct {
		OtherKey     string `yaml:"other_key"`
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
		headerCheck := func(confStr string) {
			Expect(collector.HasValidHeader(confStr)).To(BeTrue())
		}

		It("reads from bootargs and can query", func() {
			err := os.WriteFile(filepath.Join(d, "b"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(collector.MergeBootLine,
				collector.WithBootCMDLineFile(filepath.Join(d, "b")),
				collector.NoLogs, collector.StrictValidation(false))
			Expect(err).ToNot(HaveOccurred())

			headerCheck(c.String())

			Expect(c.Options["foo"]).To(Equal("bar"))
			Expect(c.Query("options")).To(Equal("foo: bar\n"))
			Expect(c.Query("options.foo")).To(Equal("bar\n"))
		})

		It("reads multiple config files", func() {
			var cc = `#kairos-config
baz: bar
kairos:
  network_token: foo
`
			var c2 = `
b: f
c: d
`

			err := os.WriteFile(filepath.Join(d, "test.yaml"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(d, "b.yaml"), []byte(c2), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(collector.Directories(d), collector.NoLogs, collector.StrictValidation(false))
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

			var cc = `#kairos-config
baz: bar
kairos:
    network_token: foo
`

			err := os.WriteFile(filepath.Join(d, "test.yaml"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = os.WriteFile(filepath.Join(d, "b.yaml"), []byte(`
fooz:
			`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(d, "more-kairos.yaml"), []byte(`#cloud-config
kairos:
    other_key: test
`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(collector.Directories(d), collector.NoLogs, collector.StrictValidation(false))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			providerCfg := &TConfig{}
			err = c.Unmarshal(providerCfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(providerCfg.Kairos).ToNot(BeNil())
			Expect(providerCfg.Kairos.NetworkToken).To(Equal("foo"))
			Expect(providerCfg.Kairos.OtherKey).To(Equal("test"))
			expectedString := `#cloud-config
baz: bar
kairos:
    network_token: foo
    other_key: test
`
			Expect(c.String()).To(Equal(expectedString), c.String(), cc)
		})

		FIt("merges with bootargs", func() {

			var cc = `#kairos-config
kairos:
  network_token: "foo"

bb: 
  nothing: "foo"
`

			err := os.WriteFile(filepath.Join(d, "test.yaml"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = os.WriteFile(filepath.Join(d, "b"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(collector.Directories(d), collector.MergeBootLine,
				collector.WithBootCMDLineFile(filepath.Join(d, "b")),
				collector.NoLogs, collector.StrictValidation(false))
			fmt.Printf("c = %T , %#v", c, c)
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
			var cc = `#cloud-config
config_url: "https://gist.githubusercontent.com/mudler/ab26e8dd65c69c32ab292685741ca09c/raw/bafae390eae4e6382fb1b68293568696823b3103/test.yaml"
`

			err := os.WriteFile(filepath.Join(d, "test.yaml"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(collector.Directories(d), collector.NoLogs, collector.StrictValidation(false))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			Expect(len(c.Bundles)).To(Equal(1))
			Expect(c.Bundles[0].Targets[0]).To(Equal("package:utils/edgevpn"))
			Expect(c.String()).ToNot(Equal(cc))
		})

		It("keeps header", func() {
			var cc = `#cloud-config
config_url: "https://gist.githubusercontent.com/mudler/7e3d0426fce8bfaaeb2644f83a9bfe0c/raw/77ded58aab3ee2a8d4117db95e078f81fd08dfde/testgist.yaml"
`

			err := os.WriteFile(filepath.Join(d, "test.yaml"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(collector.Directories(d), collector.NoLogs, collector.StrictValidation(false))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			Expect(len(c.Bundles)).To(Equal(1))
			Expect(c.Bundles[0].Targets[0]).To(Equal("package:utils/edgevpn"))
			Expect(c.String()).ToNot(Equal(cc))

			headerCheck(c.String())
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

		It("can find a top level key", func() {
			r, err := FindYAMLWithKey("a", collector.Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(Equal([]string{c1Path}))
		})

		It("can find a nested key", func() {
			r, err := FindYAMLWithKey("d.e", collector.Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(Equal([]string{c1Path}))
		})

		It("returns multiple files when key exists in them", func() {
			r, err := FindYAMLWithKey("b.c", collector.Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(ContainElements(c1Path, c2Path))
		})

		It("return an empty list when key is not found", func() {
			r, err := FindYAMLWithKey("does.not.exist", collector.Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(r).To(BeEmpty())
		})
	})
})
