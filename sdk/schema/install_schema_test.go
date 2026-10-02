package schema_test

import (
	"encoding/json"
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"strings"
)

var _ = Describe("Install Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, InstallSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when device is auto", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: auto`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when device has 'special' characters", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: "/dev/disk/by-path/pci-0000:03:00.0-scsi-0:0:0:0"`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})
	})

	Context("when device is a path", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when device is other than a path or auto", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: foobar`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).
				To(ContainSubstring("does not match pattern '^(auto|/dev/.+|script://.+)$'"))
		})
	})

	Context("when reboot and poweroff are true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
reboot: true
poweroff: true`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp("value must be false"))
		})
	})

	Context("when reboot is true and poweroff is false", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
reboot: true
poweroff: false`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when reboot is false and poweroff is true", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
reboot: false
poweroff: true`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with no power management set", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when no-format is set", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: /dev/sda
no-format: true`
		})

		It("succeedes", func() {
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		})
	})

	Context("with all possible options", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
device: "/dev/sda"
reboot: true
auto: true
image: "docker:.."
bundles:
  - rootfs_path: /usr/local/lib/extensions/<name>
    targets:
    - container://<image>
grub_options:
  extra_cmdline: "config_url=http://"
  extra_active_cmdline: "config_url=http://"
  extra_passive_cmdline: "config_url=http://"
  default_menu_entry: "foobar"
env:
  - foo=barevice: /dev/sda`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with selinux enabled and mode enforcing", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true
  mode: enforcing`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with selinux enabled and mode permissive", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true
  mode: permissive`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with selinux enabled and no mode (permissive is the default)", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("with an invalid selinux mode", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
selinux:
  enabled: true
  mode: paranoid`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp(`value must be one of "enforcing", "permissive"`))
		})
	})
})

// The install block's image-source keys used to disagree with both the runtime
// and the docs: the schema declared `install.image`, which nothing reads, and
// omitted `install.source` and `install.system.source`. These specs pin the
// declarations so the drift cannot come back silently. See kairos-io/kairos#4693.
var _ = Describe("Install Schema image source keys", func() {
	// properties walks the generated schema by key path rather than matching
	// substrings, so the specs describe the declaration itself and not the
	// JSON indentation GenerateSchema happens to emit.
	properties := func(path ...string) map[string]interface{} {
		raw, err := GenerateSchema(InstallSchema{}, "")
		Expect(err).ToNot(HaveOccurred())
		var doc map[string]interface{}
		Expect(json.Unmarshal([]byte(raw), &doc)).To(Succeed())

		// The image slots are emitted as $ref into definitions rather than
		// inlined, so resolve one hop before reading properties. Following
		// the ref is part of the assertion: it proves the slot points at a
		// definition that declares the key, not just that some definition
		// somewhere does.
		deref := func(node map[string]interface{}) map[string]interface{} {
			ref, ok := node["$ref"].(string)
			if !ok {
				return node
			}
			name := strings.TrimPrefix(ref, "#/definitions/")
			Expect(name).ToNot(Equal(ref), "unexpected $ref form %q", ref)
			defs, ok := doc["definitions"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "$ref %q with no definitions block", ref)
			target, ok := defs[name].(map[string]interface{})
			Expect(ok).To(BeTrue(), "$ref %q does not resolve", ref)
			return target
		}

		node := doc
		for _, p := range path {
			props, ok := node["properties"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "no properties on the way to %v", path)
			child, ok := props[p].(map[string]interface{})
			Expect(ok).To(BeTrue(), "%q is not declared", p)
			node = deref(child)
		}
		props, ok := node["properties"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "no properties at %v", path)
		return props
	}

	It("declares source, the key the installer reads", func() {
		source, ok := properties()["source"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "install.source is not declared")
		Expect(source["type"]).To(Equal("string"))
		Expect(source).ToNot(HaveKey("deprecated"))
	})

	It("still declares image, but marked deprecated", func() {
		image, ok := properties()["image"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "install.image was dropped rather than deprecated")
		Expect(image["deprecated"]).To(BeTrue())
		Expect(image["description"]).To(ContainSubstring("never read by the installer"))
	})

	It("declares nousers", func() {
		nousers, ok := properties()["nousers"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "install.nousers is not declared")
		Expect(nousers["type"]).To(Equal("boolean"))
	})

	DescribeTable("declares source on every image slot, and deprecates uri",
		func(slot string) {
			slotProps := properties(slot)

			source, ok := slotProps["source"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "%s.source is not declared", slot)
			Expect(source["type"]).To(Equal("string"))
			Expect(source).ToNot(HaveKey("deprecated"))

			uri, ok := slotProps["uri"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "%s.uri was dropped rather than deprecated", slot)
			Expect(uri["deprecated"]).To(BeTrue())
		},
		Entry("system", "system"),
		Entry("recovery-system", "recovery-system"),
		Entry("passive", "passive"),
	)

	DescribeTable("accepts the keys the runtime and the docs use",
		func(yaml string) {
			config, err := NewConfigFromYAML(yaml, InstallSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
		},
		Entry("top-level source", "#cloud-config\nsource: oci://quay.io/kairos/opensuse:latest"),
		Entry("nousers", "#cloud-config\nnousers: true"),
		Entry("system.source, as docs/reference/configuration.md shows it", "#cloud-config\nsystem:\n  source: \"oci:..\"\n  size: 4096"),
		Entry("recovery-system.source", "#cloud-config\nrecovery-system:\n  source: \"oci:..\"\n  size: 5000"),
		Entry("passive.source", "#cloud-config\npassive:\n  source: \"oci:..\""),
		Entry("system.uri, still honoured", "#cloud-config\nsystem:\n  uri: \"oci:..\""),
	)

	DescribeTable("rejects a wrongly typed value, so the declarations are real",
		func(yaml string) {
			config, err := NewConfigFromYAML(yaml, InstallSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeFalse(),
				"the key was accepted with the wrong type, so it is not declared")
		},
		Entry("source is a string", "#cloud-config\nsource: 5"),
		Entry("nousers is a boolean", "#cloud-config\nnousers: notabool"),
		Entry("system.source is a string", "#cloud-config\nsystem:\n  source: 5"),
	)
})
