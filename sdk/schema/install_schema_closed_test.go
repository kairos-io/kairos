package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Install block closed to undeclared keys", func() {
	validate := func(yaml string) *KConfig {
		config, err := NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		return config
	}

	DescribeTable("reports a key the install block does not declare",
		func(key string) {
			config := validate(`#cloud-config
users:
- name: kairos
install:
  device: /dev/sda
  ` + key + `: foo`)

			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(ContainSubstring(key))
		},
		// kairos-io/kairos#5833: an old doc named it, nothing reads it, and
		// the partitions were installed unencrypted.
		Entry("the never-read encryption", "encryption"),
		Entry("the underscore spelling of recovery-system", "recovery_system"),
		Entry("the dash spelling of encrypted_partitions", "encrypted-partitions"),
		Entry("the underscore spelling of extra-partitions", "extra_partitions"),
		Entry("the dash spelling of grub_options", "grub-options"),
		Entry("a key that belongs to the upgrade block", "entry"),
	)

	It("still accepts every key the block declares", func() {
		config := validate(`#cloud-config
users:
- name: kairos
  ssh_authorized_keys:
  - github:kairos
install:
  auto: true
  device: /dev/sda
  reboot: true
  no-format: true
  force: true
  nousers: true
  ssh_hardening: true
  source: "oci:quay.io/kairos/opensuse:latest"
  bind_mounts:
  - /var/lib/longhorn
  ephemeral_mounts:
  - /var/cache
  encrypted_partitions:
  - COS_PERSISTENT
  env:
  - FOO=bar
  bundles:
  - targets:
    - run://quay.io/kairos/community-bundles:kubevirt_latest
  extensions:
  - tailscale
  grub_options:
    extra_cmdline: "console=ttyS0"
  selinux:
    enabled: true
    mode: permissive
  partitions:
    persistent:
      size: 0
      fs: ext4
  extra-partitions:
  - name: data
    size: 100
    fs: ext4
  grub-entry-name: Kairos
  extra-dirs-rootfs:
  - /var/lib/longhorn
  oem_files:
  - name: foo
    content: "#cloud-config"
  system:
    uri: "oci:quay.io/kairos/opensuse:latest"
  recovery-system:
    uri: "oci:quay.io/kairos/opensuse:latest"
  passive:
    size: 3000
  firmware: efi
  part-table: gpt
  cloud-init:
  - /oem/extra.yaml
  iso: https://example.org/kairos.iso
  tty: ttyS0
  skip-entries:
  - fallback
  allow-insecure-registries: true
  registry-auth:
    username: kairos
    password: kairos`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	// reboot and poweroff are declared on the block itself so that closing it
	// does not refuse them, while PowerManagement's branches still refuse the
	// two together.
	DescribeTable("handles the power management keys",
		func(keys string, valid bool) {
			config := validate(`#cloud-config
users:
- name: kairos
install:
  device: /dev/sda
` + keys)

			if valid {
				Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
			} else {
				Expect(config.IsValid()).To(BeFalse())
			}
		},
		Entry("accepts reboot alone", "  reboot: true", true),
		Entry("accepts poweroff alone", "  poweroff: true", true),
		Entry("accepts both false", "  reboot: false\n  poweroff: false", true),
		Entry("refuses both true", "  reboot: true\n  poweroff: true", false),
		Entry("refuses a reboot that is not a boolean", "  reboot: later", false),
	)

	It("accepts the deprecated keys, which the agent warns about instead", func() {
		config := validate(`#cloud-config
users:
- name: kairos
install:
  device: /dev/sda
  no_format: true
  image: "oci:quay.io/kairos/opensuse:latest"`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})

	It("leaves a config with no install block alone", func() {
		config := validate(`#cloud-config
users:
- name: kairos`)

		Expect(config.IsValid()).To(BeTrue(), func() string { return config.ValidationError.Error() })
	})
})
