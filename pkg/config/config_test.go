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
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type TConfig struct {
	Kairos struct {
		NetworkToken string `yaml:"network_token"`
	} `yaml:"kairos"`
}

var _ = Describe("Get config", func() {
	Context("directory", func() {
		It("reads config file greedly", func() {

			var cc string = `#kairos-config
baz: bar
kairos:
  network_token: foo
`
			d, _ := ioutil.TempDir("", "xxxx")
			defer os.RemoveAll(d)

			err := ioutil.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = ioutil.WriteFile(filepath.Join(d, "b"), []byte(`
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
			d, _ := ioutil.TempDir("", "xxxx")
			defer os.RemoveAll(d)

			err := ioutil.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = ioutil.WriteFile(filepath.Join(d, "b"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
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
			d, _ := ioutil.TempDir("", "xxxx")
			defer os.RemoveAll(d)

			err := ioutil.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(Directories(d))
			Expect(err).ToNot(HaveOccurred())
			Expect(c).ToNot(BeNil())
			Expect(len(c.Bundles)).To(Equal(1))
			Expect(c.Bundles[0].Targets[0]).To(Equal("package:utils/edgevpn"))
			Expect(c.String()).ToNot(Equal(cc))
		})
	})
})
