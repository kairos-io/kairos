package collector_test

import (
	. "github.com/kairos-io/kairos/pkg/config/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v1"
)

var _ = Describe("Config Collector", func() {
	Describe("Options", func() {
		var options *Options

		BeforeEach(func() {
			options = &Options{
				NoLogs: false,
			}
		})

		It("applies a defined option function", func() {
			option := func(o *Options) error {
				o.NoLogs = true
				return nil
			}

			Expect(options.NoLogs).To(BeFalse())
			Expect(options.Apply(option)).NotTo(HaveOccurred())
			Expect(options.NoLogs).To(BeTrue())
		})
	})
	Describe("MergeConfig", func() {
		var originalConfig, newConfig *Config
		BeforeEach(func() {
			originalConfig = &Config{}
			newConfig = &Config{}
		})

		Context("different keys", func() {
			BeforeEach(func() {
				err := yaml.Unmarshal([]byte("name: Mario"), originalConfig)
				Expect(err).ToNot(HaveOccurred())
				err = yaml.Unmarshal([]byte("surname: Bros"), newConfig)
				Expect(err).ToNot(HaveOccurred())
			})

			It("gets merged together", func() {
				Expect(originalConfig.MergeConfig(newConfig)).ToNot(HaveOccurred())
				surname, isString := (*originalConfig)["surname"].(string)
				Expect(isString).To(BeTrue())
				Expect(surname).To(Equal("Bros"))
			})
		})

		Context("same keys", func() {
			Context("when the key is a map", func() {
				BeforeEach(func() {
					err := yaml.Unmarshal([]byte(`---
info:
  name: Mario
`), originalConfig)
					Expect(err).ToNot(HaveOccurred())
					err = yaml.Unmarshal([]byte(`---
info:
  surname: Bros
`), newConfig)
					Expect(err).ToNot(HaveOccurred())
				})
				It("merges the keys", func() {
					Expect(originalConfig.MergeConfig(newConfig)).ToNot(HaveOccurred())
					info, isMap := (*originalConfig)["info"].(map[interface{}]interface{})
					Expect(isMap).To(BeTrue())
					Expect(info["name"]).To(Equal("Mario"))
					Expect(info["surname"]).To(Equal("Bros"))
					Expect(len(*originalConfig)).To(Equal(1))
					Expect(len(info)).To(Equal(2))
				})
			})

			Context("when the key is a string", func() {
				BeforeEach(func() {
					err := yaml.Unmarshal([]byte("name: Mario"), originalConfig)
					Expect(err).ToNot(HaveOccurred())
					err = yaml.Unmarshal([]byte("name: Luigi"), newConfig)
					Expect(err).ToNot(HaveOccurred())
				})

				It("overwrites", func() {
					Expect(originalConfig.MergeConfig(newConfig)).ToNot(HaveOccurred())
					surname, isString := (*originalConfig)["name"].(string)
					Expect(isString).To(BeTrue())
					Expect(surname).To(Equal("Luigi"))
					Expect(len(*originalConfig)).To(Equal(1))
				})
			})
		})
	})
})
