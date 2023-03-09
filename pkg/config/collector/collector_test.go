package collector_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

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
					Expect(*originalConfig).To(HaveLen(1))
					Expect(info).To(HaveLen(2))
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
					name, isString := (*originalConfig)["name"].(string)
					Expect(isString).To(BeTrue())
					Expect(name).To(Equal("Luigi"))
					Expect(*originalConfig).To(HaveLen(1))
				})
			})
		})
	})

	Describe("MergeConfigURL", func() {
		var originalConfig *Config
		BeforeEach(func() {
			originalConfig = &Config{}
		})

		Context("when there is no config_url defined", func() {
			BeforeEach(func() {
				err := yaml.Unmarshal([]byte("name: Mario"), originalConfig)
				Expect(err).ToNot(HaveOccurred())
			})

			It("does nothing", func() {
				Expect(originalConfig.MergeConfigURL()).ToNot(HaveOccurred())
				Expect(*originalConfig).To(HaveLen(1))
			})
		})

		Context("when there is a chain of config_url defined", func() {
			var closeFunc ServerCloseFunc
			var port int
			var err error
			var tmpDir string
			var originalConfig *Config

			BeforeEach(func() {
				tmpDir, err = os.MkdirTemp("", "config_url_chain")
				Expect(err).ToNot(HaveOccurred())

				closeFunc, port, err = startAssetServer(tmpDir)
				Expect(err).ToNot(HaveOccurred())

				originalConfig = &Config{}
				err = yaml.Unmarshal([]byte(fmt.Sprintf(`---
config_url: http://127.0.0.1:%d/config1.yaml
name: Mario
surname: Bros
info:
  job: plumber
`, port)), originalConfig)
				Expect(err).ToNot(HaveOccurred())

				err := os.WriteFile(path.Join(tmpDir, "config1.yaml"), []byte(fmt.Sprintf(`
---
config_url: http://127.0.0.1:%d/config2.yaml
surname: Bras
`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = os.WriteFile(path.Join(tmpDir, "config2.yaml"), []byte(`
---
info:
  girlfriend: princess
`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				closeFunc()
				err := os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
			})

			It("merges them all together", func() {
				err := originalConfig.MergeConfigURL()
				Expect(err).ToNot(HaveOccurred())

				name, ok := (*originalConfig)["name"].(string)
				Expect(ok).To(BeTrue())
				Expect(name).To(Equal("Mario"))

				surname, ok := (*originalConfig)["surname"].(string)
				Expect(ok).To(BeTrue())
				Expect(surname).To(Equal("Bras"))

				info, ok := (*originalConfig)["info"].(map[interface{}]interface{})
				Expect(ok).To(BeTrue())
				Expect(info["job"]).To(Equal("plumber"))
				Expect(info["girlfriend"]).To(Equal("princess"))

				Expect(*originalConfig).To(HaveLen(4))
			})
		})
	})

	Describe("Scan", func() {
		Context("multiple sources are defined", func() {
			var tmpDir, tmpDir1, tmpDir2 string
			var err error

			BeforeEach(func() {
				tmpDir, err = os.MkdirTemp("", "cmdline")
				Expect(err).ToNot(HaveOccurred())
				tmpDir1, err = os.MkdirTemp("", "config1")
				Expect(err).ToNot(HaveOccurred())
				tmpDir2, err = os.MkdirTemp("", "config2")
				Expect(err).ToNot(HaveOccurred())

				err := os.WriteFile(filepath.Join(tmpDir, "cmdline"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = os.WriteFile(path.Join(tmpDir1, "config.yaml"), []byte("name: Mario"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = os.WriteFile(path.Join(tmpDir2, "config.yaml"), []byte("surname: Bros"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				err = os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.RemoveAll(tmpDir1)
				Expect(err).ToNot(HaveOccurred())
				err = os.RemoveAll(tmpDir2)
				Expect(err).ToNot(HaveOccurred())
			})

			It("merges all the sources accordingly", func() {
				path := filepath.Join(tmpDir, "cmdline")
				c, err := Scan(MergeBootLine, WithBootCMDLineFile(path), Directories(tmpDir1, tmpDir2), NoLogs)
				Expect(err).ToNot(HaveOccurred())

				zz, ok := (*c)["zz"].(map[interface{}]interface{})
				Expect(ok).To(BeTrue())
				Expect(zz["foo"]).To(Equal("baa"))

				options, ok := (*c)["options"].(map[interface{}]interface{})
				Expect(ok).To(BeTrue())
				Expect(options["foo"]).To(Equal("bar"))
				Expect(err).ToNot(HaveOccurred())

				name, ok := (*c)["name"].(string)
				Expect(ok).To(BeTrue())
				Expect(name).To(Equal("Mario"))

				surname, ok := (*c)["surname"].(string)
				Expect(ok).To(BeTrue())
				Expect(surname).To(Equal("Bros"))
			})
		})
	})
})
