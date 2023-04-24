package collector_test

import (
	"fmt"
	"github.com/kairos-io/kairos/v2/pkg/config"
	"os"
	"path"
	"path/filepath"

	. "github.com/kairos-io/kairos/v2/pkg/config/collector"
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
				err := yaml.Unmarshal([]byte(`#cloud-config
name: Mario`), originalConfig)
				Expect(err).ToNot(HaveOccurred())
				err = yaml.Unmarshal([]byte(`#cloud-config
surname: Bros`), newConfig)
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
					err := yaml.Unmarshal([]byte(`#cloud-config
info:
  name: Mario
`), originalConfig)
					Expect(err).ToNot(HaveOccurred())
					err = yaml.Unmarshal([]byte(`#cloud-config
info:
  surname: Bros
`), newConfig)
					Expect(err).ToNot(HaveOccurred())
				})
				It("merges the keys", func() {
					Expect(originalConfig.MergeConfig(newConfig)).ToNot(HaveOccurred())
					info, isMap := (*originalConfig)["info"].(Config)
					Expect(isMap).To(BeTrue())
					Expect(info["name"]).To(Equal("Mario"))
					Expect(info["surname"]).To(Equal("Bros"))
					Expect(*originalConfig).To(HaveLen(1))
					Expect(info).To(HaveLen(2))
				})
			})

			Context("when the key is a string", func() {
				BeforeEach(func() {
					err := yaml.Unmarshal([]byte("#cloud-config\nname: Mario"), originalConfig)
					Expect(err).ToNot(HaveOccurred())
					err = yaml.Unmarshal([]byte("#cloud-config\nname: Luigi"), newConfig)
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
				err := yaml.Unmarshal([]byte("#cloud-config\nname: Mario"), originalConfig)
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
				err = yaml.Unmarshal([]byte(fmt.Sprintf(`#cloud-config
config_url: http://127.0.0.1:%d/config1.yaml
name: Mario
surname: Bros
info:
  job: plumber
`, port)), originalConfig)
				Expect(err).ToNot(HaveOccurred())

				err := os.WriteFile(path.Join(tmpDir, "config1.yaml"), []byte(fmt.Sprintf(`#cloud-config

config_url: http://127.0.0.1:%d/config2.yaml
surname: Bras
`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = os.WriteFile(path.Join(tmpDir, "config2.yaml"), []byte(`#cloud-config

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

				info, ok := (*originalConfig)["info"].(Config)
				Expect(ok).To(BeTrue())
				Expect(info["job"]).To(Equal("plumber"))
				Expect(info["girlfriend"]).To(Equal("princess"))

				Expect(*originalConfig).To(HaveLen(4))
			})
		})
	})

	Describe("deepMerge", func() {
		Context("different types", func() {
			a := map[string]interface{}{}
			b := []string{}

			It("merges", func() {
				_, err := DeepMerge(a, b)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("cannot merge map[string]interface {} with []string"))

				_, err = DeepMerge(b, a)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("cannot merge []string with map[string]interface {}"))
			})
		})

		Context("simple slices", func() {
			a := []interface{}{"one", "three"}
			b := []interface{}{"two", 4}

			It("merges", func() {
				c, err := DeepMerge(a, b)
				Expect(err).ToNot(HaveOccurred())
				Expect(c).To(Equal([]interface{}{"one", "three", "two", 4}))
			})
		})

		Context("slices containing maps", func() {
			a := []interface{}{
				map[string]interface{}{
					"users": []interface{}{
						map[string]interface{}{
							"kairos": map[string]interface{}{
								"passwd": "kairos",
							},
						},
					},
				},
			}
			b := []interface{}{
				map[string]interface{}{
					"users": []interface{}{
						map[string]interface{}{
							"foo": map[string]interface{}{
								"passwd": "bar",
							},
						},
					},
				},
			}

			It("merges", func() {
				c, err := DeepMerge(a, b)
				Expect(err).ToNot(HaveOccurred())
				users := c.([]interface{})[0].(map[string]interface{})["users"]
				Expect(users).To(HaveLen(2))
				Expect(users).To(ContainElement(
					map[string]interface{}{
						"kairos": map[string]interface{}{
							"passwd": "kairos",
						},
					},
				))
				Expect(users).To(ContainElement(
					map[string]interface{}{
						"foo": map[string]interface{}{
							"passwd": "bar",
						},
					},
				))
			})
		})

		Context("empty map", func() {
			a := map[string]interface{}{}
			b := map[string]interface{}{
				"foo": "bar",
			}

			It("merges", func() {
				c, err := DeepMerge(a, b)
				Expect(err).ToNot(HaveOccurred())
				Expect(c).To(Equal(map[string]interface{}{
					"foo": "bar",
				}))
			})
		})

		Context("simple map", func() {
			a := map[string]interface{}{
				"es": "uno",
				"nl": "een",
				"#":  0,
			}
			b := map[string]interface{}{
				"en": "one",
				"nl": "één",
				"de": "Eins",
				"#":  1,
			}

			It("merges", func() {
				c, err := DeepMerge(a, b)
				Expect(err).ToNot(HaveOccurred())
				Expect(c).To(Equal(map[string]interface{}{
					"#":  1,
					"de": "Eins",
					"en": "one",
					"es": "uno",
					"nl": "één",
				}))
			})
		})
	})

	Describe("Scan", func() {
		Context("Deep merge maps within arrays", func() {
			var cmdLinePath, tmpDir1 string
			var err error

			BeforeEach(func() {
				tmpDir1, err = os.MkdirTemp("", "config1")
				Expect(err).ToNot(HaveOccurred())
				err := os.WriteFile(path.Join(tmpDir1, "local_config_1.yaml"), []byte(`#cloud-config
install:
  auto: true
  reboot: false
  poweroff: false
  grub_options:
     extra_cmdline: "console=tty0"
options:
  device: /dev/sda
stages:
  initramfs:
    - users:
        kairos:
          groups:
            - sudo
          passwd: kairos
`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = os.WriteFile(path.Join(tmpDir1, "local_config_2.yaml"), []byte(`#cloud-config
stages:
  initramfs:
    - users:
        foo:
          groups:
            - sudo
          passwd: bar
`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				err = os.RemoveAll(tmpDir1)
				Expect(err).ToNot(HaveOccurred())
			})

			It("merges all the sources accordingly", func() {
				o := &Options{}
				err := o.Apply(
					MergeBootLine,
					WithBootCMDLineFile(cmdLinePath),
					Directories(tmpDir1),
				)
				Expect(err).ToNot(HaveOccurred())

				c, err := Scan(o, config.FilterKeys)
				Expect(err).ToNot(HaveOccurred())

				Expect(c.String()).To(Equal(`#cloud-config

install:
    auto: true
    grub_options:
        extra_cmdline: console=tty0
    poweroff: false
    reboot: false
options:
    device: /dev/sda
stages:
    initramfs:
        - users:
            foo:
                groups:
                    - sudo
                passwd: bar
            kairos:
                groups:
                    - sudo
                passwd: kairos
`))
			})
		})

		Context("multiple sources are defined", func() {
			var cmdLinePath, serverDir, tmpDir, tmpDir1, tmpDir2 string
			var err error
			var closeFunc ServerCloseFunc
			var port int

			BeforeEach(func() {
				// Prepare the cmdline config_url chain
				serverDir, err = os.MkdirTemp("", "config_url_chain")
				Expect(err).ToNot(HaveOccurred())
				closeFunc, port, err = startAssetServer(serverDir)
				Expect(err).ToNot(HaveOccurred())
				cmdLinePath = createRemoteConfigs(serverDir, port)

				tmpDir1, err = os.MkdirTemp("", "config1")
				Expect(err).ToNot(HaveOccurred())
				err := os.WriteFile(path.Join(tmpDir1, "local_config_1.yaml"), []byte(fmt.Sprintf(`#cloud-config

config_url: http://127.0.0.1:%d/remote_config_3.yaml
local_key_1: local_value_1
`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = os.WriteFile(path.Join(serverDir, "remote_config_3.yaml"), []byte(fmt.Sprintf(`#cloud-config

config_url: http://127.0.0.1:%d/remote_config_4.yaml
remote_key_3: remote_value_3
`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = os.WriteFile(path.Join(serverDir, "remote_config_4.yaml"), []byte(`#cloud-config

options:
  remote_option_1: remote_option_value_1
`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				tmpDir2, err = os.MkdirTemp("", "config2")
				Expect(err).ToNot(HaveOccurred())
				err = os.WriteFile(path.Join(tmpDir2, "local_config_2.yaml"), []byte(fmt.Sprintf(`#cloud-config

config_url: http://127.0.0.1:%d/remote_config_5.yaml
local_key_2: local_value_2
`, port)), os.ModePerm)
				err = os.WriteFile(path.Join(tmpDir2, "local_config_3.yaml"), []byte(`#cloud-config
local_key_3: local_value_3
`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
				err = os.WriteFile(path.Join(serverDir, "remote_config_5.yaml"), []byte(fmt.Sprintf(`#cloud-config

config_url: http://127.0.0.1:%d/remote_config_6.yaml
remote_key_4: remote_value_4
`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				err = os.WriteFile(path.Join(serverDir, "remote_config_6.yaml"), []byte(`#cloud-config

options:
  remote_option_2: remote_option_value_2
`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				err = os.RemoveAll(serverDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.RemoveAll(tmpDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.RemoveAll(tmpDir1)
				Expect(err).ToNot(HaveOccurred())
				err = os.RemoveAll(tmpDir2)
				Expect(err).ToNot(HaveOccurred())

				closeFunc()
			})

			It("merges all the sources accordingly", func() {
				o := &Options{}
				err := o.Apply(
					MergeBootLine,
					WithBootCMDLineFile(cmdLinePath),
					Directories(tmpDir1, tmpDir2),
				)
				Expect(err).ToNot(HaveOccurred())

				c, err := Scan(o, config.FilterKeys)
				Expect(err).ToNot(HaveOccurred())

				configURL, ok := (*c)["config_url"].(string)
				Expect(ok).To(BeTrue())
				Expect(configURL).To(MatchRegexp("remote_config_2.yaml"))

				k := (*c)["local_key_1"].(string)
				Expect(k).To(Equal("local_value_1"))
				k = (*c)["local_key_2"].(string)
				Expect(k).To(Equal("local_value_2"))
				k = (*c)["local_key_3"].(string)
				Expect(k).To(Equal("local_value_3"))
				k = (*c)["remote_key_1"].(string)
				Expect(k).To(Equal("remote_value_1"))
				k = (*c)["remote_key_2"].(string)
				Expect(k).To(Equal("remote_value_2"))
				k = (*c)["remote_key_3"].(string)
				Expect(k).To(Equal("remote_value_3"))
				k = (*c)["remote_key_4"].(string)
				Expect(k).To(Equal("remote_value_4"))

				options := (*c)["options"].(Config)
				Expect(options["foo"]).To(Equal("bar"))
				Expect(options["remote_option_1"]).To(Equal("remote_option_value_1"))
				Expect(options["remote_option_2"]).To(Equal("remote_option_value_2"))

				player := (*c)["player"].(Config)
				Expect(player["name"]).NotTo(Equal("Toad"))
				Expect(player["surname"]).To(Equal("Bros"))
			})
		})

		Context("when files have invalid or missing headers", func() {
			var serverDir, tmpDir string
			var err error
			var closeFunc ServerCloseFunc
			var port int

			BeforeEach(func() {
				// Prepare the cmdline config_url chain
				serverDir, err = os.MkdirTemp("", "config_url_chain")
				Expect(err).ToNot(HaveOccurred())
				closeFunc, port, err = startAssetServer(serverDir)
				Expect(err).ToNot(HaveOccurred())

				tmpDir, err = os.MkdirTemp("", "config")
				Expect(err).ToNot(HaveOccurred())

				// Local configs
				err = os.WriteFile(path.Join(tmpDir, "local_config.yaml"), []byte(fmt.Sprintf(`#cloud-config
config_url: http://127.0.0.1:%d/remote_config_1.yaml
local_key_1: local_value_1
`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				// missing header
				err = os.WriteFile(path.Join(tmpDir, "local_config_2.yaml"),
					[]byte("local_key_2: local_value_2"), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				// Remote config with valid header
				err := os.WriteFile(path.Join(serverDir, "remote_config_1.yaml"), []byte(fmt.Sprintf(`#cloud-config
config_url: http://127.0.0.1:%d/remote_config_2.yaml
remote_key_1: remote_value_1`, port)), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())

				// Remote config with invalid header
				err = os.WriteFile(path.Join(serverDir, "remote_config_2.yaml"), []byte(`#invalid-header
remote_key_2: remote_value_2`), os.ModePerm)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				closeFunc()
				err = os.RemoveAll(serverDir)
				Expect(err).ToNot(HaveOccurred())
				err = os.RemoveAll(tmpDir)
			})

			It("ignores them", func() {
				o := &Options{}
				err := o.Apply(Directories(tmpDir), NoLogs)
				Expect(err).ToNot(HaveOccurred())

				c, err := Scan(o, config.FilterKeys)
				Expect(err).ToNot(HaveOccurred())

				Expect((*c)["local_key_2"]).To(BeNil())
				Expect((*c)["remote_key_2"]).To(BeNil())

				// sanity check, the rest should be there
				v, ok := (*c)["config_url"].(string)
				Expect(ok).To(BeTrue())
				Expect(v).To(MatchRegexp("remote_config_2.yaml"))

				v, ok = (*c)["local_key_1"].(string)
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("local_value_1"))

				v, ok = (*c)["remote_key_1"].(string)
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("remote_value_1"))
			})
		})
	})

	Describe("String", func() {
		var conf *Config
		BeforeEach(func() {
			conf = &Config{}
			err := yaml.Unmarshal([]byte("name: Mario"), conf)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns the YAML string representation of the Config", func() {
			s, err := conf.String()
			Expect(err).ToNot(HaveOccurred())
			Expect(s).To(Equal(`#cloud-config

name: Mario
`), s)
		})
	})

	Describe("Query", func() {
		var tmpDir string
		var err error

		BeforeEach(func() {
			tmpDir, err = os.MkdirTemp("", "config")
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(tmpDir, "b"), []byte(`zz.foo="baa" options.foo=bar`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(path.Join(tmpDir, "local_config.yaml"), []byte(`#cloud-config
local_key_1: local_value_1
some:
  other:
    key: 3
`), os.ModePerm)
			Expect(err).ToNot(HaveOccurred())
		})

		It("can query for keys", func() {
			o := &Options{}

			err = o.Apply(MergeBootLine, Directories(tmpDir),
				WithBootCMDLineFile(filepath.Join(tmpDir, "b")),
			)
			Expect(err).ToNot(HaveOccurred())

			c, err := Scan(o, config.FilterKeys)
			Expect(err).ToNot(HaveOccurred())

			v, err := c.Query("local_key_1")
			Expect(err).ToNot(HaveOccurred())
			Expect(v).To(Equal("local_value_1\n"))
			v, err = c.Query("some")
			Expect(err).ToNot(HaveOccurred())
			Expect(v).To(Equal("other:\n    key: 3\n"))
			v, err = c.Query("some.other")
			Expect(v).To(Equal("key: 3\n"))
			v, err = c.Query("some.other.key")
			Expect(v).To(Equal("3\n"))
			Expect(c.Query("options")).To(Equal("foo: bar\n"))
		})
	})
})

func createRemoteConfigs(serverDir string, port int) string {
	err := os.WriteFile(path.Join(serverDir, "remote_config_1.yaml"), []byte(fmt.Sprintf(`#cloud-config

config_url: http://127.0.0.1:%d/remote_config_2.yaml
player:
remote_key_1: remote_value_1
`, port)), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())
	err = os.WriteFile(path.Join(serverDir, "remote_config_2.yaml"), []byte(`#cloud-config

player:
  surname: Bros
remote_key_2: remote_value_2
`), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	cmdLinePath := filepath.Join(serverDir, "cmdline")
	// We put the cmdline in the same dir, it doesn't matter.
	cmdLine := fmt.Sprintf(`config_url="http://127.0.0.1:%d/remote_config_1.yaml" player.name="Toad" options.foo=bar`, port)
	err = os.WriteFile(cmdLinePath, []byte(cmdLine), os.ModePerm)
	Expect(err).ToNot(HaveOccurred())

	return cmdLinePath
}
