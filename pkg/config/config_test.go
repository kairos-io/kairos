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

	. "github.com/c3os-io/c3os/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("Get config", func() {
	Context("directory", func() {
		It("reads config file greedly", func() {

			var cc string = `baz: bar
c3os:
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
			Expect(c.C3OS.NetworkToken).To(Equal("foo"))
			Expect(c.String()).To(Equal(cc))
		})

		It("replace token in config files", func() {

			var cc string = `
c3os:
  network_token: "foo"

bb: 
  nothing: "foo"
`
			d, _ := ioutil.TempDir("", "xxxx")
			defer os.RemoveAll(d)

			err := ioutil.WriteFile(filepath.Join(d, "test"), []byte(cc), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
			err = ioutil.WriteFile(filepath.Join(d, "b"), []byte(`
fooz:
			`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = ReplaceToken([]string{d, "/doesnotexist"}, "baz")
			Expect(err).ToNot(HaveOccurred())

			content, err := ioutil.ReadFile(filepath.Join(d, "test"))
			Expect(err).ToNot(HaveOccurred())

			res := map[interface{}]interface{}{}

			err = yaml.Unmarshal(content, &res)
			Expect(err).ToNot(HaveOccurred())

			Expect(res).To(Equal(map[interface{}]interface{}{
				"c3os": map[interface{}]interface{}{"network_token": "baz"},
				"bb":   map[interface{}]interface{}{"nothing": "foo"},
			}))
		})

		It("merges with bootargs", func() {

			var cc string = `
c3os:
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
			Expect(c.C3OS.NetworkToken).To(Equal("foo"))
			_, exists := c.Data()["zz"]
			Expect(exists).To(BeFalse())
		})
	})
})
