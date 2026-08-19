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
	"reflect"
	"strings"

	pkgConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-sdk/collector"
	"github.com/kairos-io/kairos-sdk/schema"
	sdkbundles "github.com/kairos-io/kairos-sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkImages "github.com/kairos-io/kairos-sdk/types/images"
	sdkInstall "github.com/kairos-io/kairos-sdk/types/install"
	sdkPartitions "github.com/kairos-io/kairos-sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

func getTagName(s string) string {
	if len(s) < 1 {
		return ""
	}

	if s == "-" {
		return ""
	}

	f := func(c rune) bool {
		return c == '"' || c == ','
	}
	index := strings.IndexFunc(s, f)
	if index == -1 {
		return s
	}

	return s[:index]
}

func structContainsField(f, t string, str interface{}) bool {
	values := reflect.ValueOf(str)
	types := values.Type()

	for j := 0; j < values.NumField(); j++ {
		tagName := getTagName(types.Field(j).Tag.Get("json"))
		if types.Field(j).Name == f || tagName == t {
			return true
		} else {
			if types.Field(j).Type.Kind() == reflect.Struct {
				if types.Field(j).Type.Name() != "" {
					model := reflect.New(types.Field(j).Type)
					if instance, ok := model.Interface().(schema.OneOfModel); ok {
						for _, childSchema := range instance.JSONSchemaOneOf() {
							if structContainsField(f, t, childSchema) {
								return true
							}
						}
					}
				}
			}
		}
	}

	return false
}

func structFieldsContainedInOtherStruct(left, right interface{}) {
	leftValues := reflect.ValueOf(left)
	leftTypes := leftValues.Type()

	for i := 0; i < leftValues.NumField(); i++ {
		leftTagName := getTagName(leftTypes.Field(i).Tag.Get("yaml"))
		leftFieldName := leftTypes.Field(i).Name
		if leftTypes.Field(i).IsExported() {
			It(fmt.Sprintf("Checks that the new schema contains the field %s", leftFieldName), func() {
				if leftFieldName == "Source" || leftFieldName == "NoUsers" || leftFieldName == "BindPublicPCRs" || leftFieldName == "BindPCRs" || leftFieldName == "Logs" {
					Skip("Schema not updated yet")
				}
				Expect(
					structContainsField(leftFieldName, leftTagName, right),
				).To(BeTrue())
			})
		}
	}
}

var _ = Describe("Schema", func() {
	Context("NewConfigFromYAML", func() {
		Context("While the new Schema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(sdkConfig.Config{}, schema.RootSchema{})
		})
		Context("While the new InstallSchema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(sdkInstall.Install{}, schema.InstallSchema{})
		})
		Context("While the new BundleSchema is not the single source of truth", func() {
			structFieldsContainedInOtherStruct(sdkbundles.Bundle{}, schema.BundleSchema{})
		})
	})

	Describe("Install unmarshall for payloads", func() {
		It("produces a yaml without empty fields", func() {
			wants := `install:
    poweroff: true
    bind_mounts:
        - /var/lib/ceph
        - /var/lib/osd
    partitions:
        oem:
            size: 5120
            fs: ext4
    system:
        size: 8192
    recovery-system:
        size: 10000
    passive:
        size: 8192
`
			config := sdkConfig.Config{
				Install: &sdkInstall.Install{
					Poweroff: true,
					BindMounts: []string{
						"/var/lib/ceph",
						"/var/lib/osd",
					},
					Active: sdkImages.Image{
						Size: 8192,
					},
					Passive: sdkImages.Image{
						Size: 8192,
					},
					Recovery: sdkImages.Image{
						Size: 10000,
					},
					Partitions: sdkPartitions.ElementalPartitions{
						OEM: &sdkPartitions.Partition{
							Size: 5120,
							FS:   "ext4",
						},
					},
				},
			}

			got, err := yaml.Marshal(config)
			Expect(Expect(err).NotTo(HaveOccurred()))

			Expect(string(got)).To(Equal(wants))
		})
	})

	Describe("Validate users in config", func() {
		It("Validates a existing user in the system", func() {
			cc := `#cloud-config
stages:
  initramfs:
    - name: "Set user and password"
      users:
        kairos:
          passwd: "kairos"
          groups:
            - "admin"
`
			c, err := pkgConfig.ScanNoLogs(collector.Readers(strings.NewReader(cc)))
			Expect(err).ToNot(HaveOccurred())
			Expect(pkgConfig.CheckConfigForUsers(c)).ToNot(HaveOccurred())
		})
		It("Fails if there is no user", func() {
			c, err := pkgConfig.ScanNoLogs(collector.NoLogs)
			Expect(err).ToNot(HaveOccurred())
			Expect(pkgConfig.CheckConfigForUsers(c)).To(HaveOccurred())
		})
		It("Fails if there is user but its not admin", func() {
			cc := `#cloud-config
stages:
  initramfs:
    - name: "Set user and password"
      users:
        kairos:
          passwd: "kairos"
`
			c, err := pkgConfig.ScanNoLogs(collector.Readers(strings.NewReader(cc)))
			Expect(err).ToNot(HaveOccurred())
			Expect(pkgConfig.CheckConfigForUsers(c)).To(HaveOccurred())
		})
	})

	Describe("Platform handling", func() {
		It("creates a config for riscv64 platform values", func() {
			c := pkgConfig.NewConfig(pkgConfig.WithPlatform("linux/riscv64"))
			Expect(c).ToNot(BeNil())
			Expect(c.Platform).ToNot(BeNil())
			Expect(c.Platform.OS).To(Equal("linux"))
			Expect(c.Platform.Arch).To(Equal("riscv64"))
			Expect(c.Platform.GolangArch).To(Equal("riscv64"))
		})
	})

	Describe("YAML helpers", func() {
		It("merges multiple YAML objects", func() {
			merged, err := pkgConfig.MergeYAML(
				map[string]string{"a": "1"},
				map[string]string{"b": "2"},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(merged)).To(ContainSubstring("a: \"1\""))
			Expect(string(merged)).To(ContainSubstring("b: \"2\""))
		})

		It("adds a header line before data", func() {
			Expect(pkgConfig.AddHeader("#cloud-config", "phonehome:\n  url: x")).To(Equal("#cloud-config\nphonehome:\n  url: x"))
		})

		It("stringifies known stages", func() {
			Expect(pkgConfig.NetworkStage.String()).To(Equal("network"))
			Expect(pkgConfig.InitramfsStage.String()).To(Equal("initramfs"))
		})
	})

	Describe("CheckConfigForExtraPartitions", func() {
		It("accepts extra partitions with names", func() {
			c := &sdkConfig.Config{
				Install: &sdkInstall.Install{
					ExtraPartitions: sdkPartitions.PartitionList{
						{Name: "data"},
					},
				},
			}
			Expect(pkgConfig.CheckConfigForExtraPartitions(c)).To(Succeed())
		})

		It("rejects extra partitions without names", func() {
			c := &sdkConfig.Config{
				Install: &sdkInstall.Install{
					ExtraPartitions: sdkPartitions.PartitionList{
						{Size: 1024},
					},
				},
			}
			Expect(pkgConfig.CheckConfigForExtraPartitions(c)).To(MatchError(ContainSubstring("without a name")))
		})
	})
})
