package schema_test

import (
	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SuggestUnknownKeys", func() {
	var suggestions []string
	var yaml string

	JustBeforeEach(func() {
		config, err := NewConfigFromYAML(yaml, RootSchema{})
		Expect(err).ToNot(HaveOccurred())
		suggestions = config.SuggestUnknownKeys()
	})

	Context("when a top-level key is a misspelling of a schema key", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
uses:
  - name: kairos
    passwd: kairos`
		})

		It("names the key and the one it is close to", func() {
			Expect(suggestions).To(ConsistOf(
				`unknown configuration key "uses", which is ignored; did you mean "users"?`))
		})

		It("does not make the configuration invalid", func() {
			// The whole point of a suggestion is that it costs nothing to
			// emit: a config that validates today still validates.
			config, err := NewConfigFromYAML(yaml, RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("when every top-level key is declared by the schema", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
install:
  device: /dev/sda
users:
  - name: kairos
p2p:
  disable_dht: true
debug: true
stages:
  boot:
    - commands:
        - echo hello`
		})

		It("suggests nothing", func() {
			Expect(suggestions).To(BeEmpty())
		})
	})

	Context("when a top-level key is outside the schema but close to nothing in it", func() {
		BeforeEach(func() {
			// These are all real: the Kubernetes blocks are read by the
			// provider, and the cloud-init keys by yip. None of them is in
			// RootSchema, and none of them is a misspelling of anything in
			// it, so reporting them would be noise on a correct config.
			yaml = `#cloud-config
k3s:
  enabled: true
k3s-agent:
  enabled: true
k0s:
  enabled: true
k0s-worker:
  enabled: true
hostname: node-1
write_files:
  - path: /tmp/a
runcmd:
  - echo hello
growpart:
  devices: ["/"]
name: somenode
bootcmd:
  - echo early`
		})

		It("suggests nothing", func() {
			Expect(suggestions).To(BeEmpty())
		})
	})

	Context("when several keys are misspelled", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
instal:
  device: /dev/sda
bundle: []
stage: {}`
		})

		It("reports each one, in a stable order", func() {
			Expect(suggestions).To(Equal([]string{
				`unknown configuration key "bundle", which is ignored; did you mean "bundles"?`,
				`unknown configuration key "instal", which is ignored; did you mean "install"?`,
				`unknown configuration key "stage", which is ignored; did you mean "stages"?`,
			}))
		})
	})

	Context("when a key differs only in punctuation", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
kube-vip:
  eip: 192.168.1.1`
		})

		It("suggests the schema spelling", func() {
			Expect(suggestions).To(ConsistOf(
				`unknown configuration key "kube-vip", which is ignored; did you mean "kubevip"?`))
		})
	})

	Context("when a key differs only in case", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
Users:
  - name: kairos`
		})

		It("suggests the schema spelling", func() {
			Expect(suggestions).To(ConsistOf(
				`unknown configuration key "Users", which is ignored; did you mean "users"?`))
		})
	})

	Context("when two edits would be needed on a short key", func() {
		BeforeEach(func() {
			// "ntp" is a cloud-init key, and two edits from "p2p": at
			// three characters that is most of the key rewritten, so the
			// nearest match is a coincidence rather than a correction.
			// Without the length guard this reports `did you mean "p2p"?`
			// on a correct configuration.
			yaml = `#cloud-config
ntp:
  enabled: true`
		})

		It("suggests nothing", func() {
			Expect(suggestions).To(BeEmpty())
		})
	})

	Context("when two edits would be needed on a longer key", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
squash-compresion: ["-comp", "xz"]`
		})

		It("suggests the schema spelling", func() {
			Expect(suggestions).To(ConsistOf(
				`unknown configuration key "squash-compresion", which is ignored; did you mean "squash-compression"?`))
		})
	})

	Context("when a key is the same distance from two schema keys", func() {
		BeforeEach(func() {
			// "cosign-k" is two edits from "cosign" and two from
			// "cosign-key". Either would be a fair suggestion, so the only
			// thing that matters is that the same one is named every time.
			yaml = `#cloud-config
cosign-k: true`
		})

		It("names the same one every time", func() {
			Expect(suggestions).To(ConsistOf(
				`unknown configuration key "cosign-k", which is ignored; did you mean "cosign"?`))
		})
	})

	Context("when the document is not a mapping", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
- one
- two`
		})

		It("suggests nothing", func() {
			Expect(suggestions).To(BeEmpty())
		})
	})

	Context("when the document is empty", func() {
		BeforeEach(func() {
			yaml = `#cloud-config`
		})

		It("suggests nothing", func() {
			Expect(suggestions).To(BeEmpty())
		})
	})

	Context("when a schema key is added", func() {
		It("is suggested without this test naming it", func() {
			// The key list is read off the generated schema document, so it
			// follows RootSchema. This asserts the mechanism rather than a
			// hand-kept copy of the field names: a misspelling of any
			// declared top-level key resolves back to that key.
			config, err := NewConfigFromYAML("#cloud-config\nkcrytp: {}\n", RootSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.SuggestUnknownKeys()).To(ConsistOf(
				`unknown configuration key "kcrytp", which is ignored; did you mean "kcrypt"?`))
		})
	})
})
