package challenger

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/validation"
)

var _ = Describe("SealedVolumeData.DefaultSecret", func() {
	Context("when the operator named the Secret in the SealedVolume", func() {
		// SecretName and SecretPath are copied out of the operator's SealedVolume
		// by findPartitionInVolume. They point at an object the operator already
		// created, so the challenger has to ask for them exactly as written.
		It("uses a Secret name with a dot as written", func() {
			// "kcrypt.passphrase" is a legal RFC 1123 subdomain, so it is a legal
			// Secret name.
			Expect(validation.IsDNS1123Subdomain("kcrypt.passphrase")).To(BeEmpty())

			v := SealedVolumeData{
				VolumeName:     "tofu-a1b2c3d4",
				PartitionLabel: "COS_PERSISTENT",
				SecretName:     "kcrypt.passphrase",
			}
			name, _ := v.DefaultSecret()
			Expect(name).To(Equal("kcrypt.passphrase"))
		})

		It("uses a Secret key with an underscore or a dot as written", func() {
			// Secret keys allow "." and "_", they are not object names.
			Expect(validation.IsConfigMapKey("pass_phrase")).To(BeEmpty())
			Expect(validation.IsConfigMapKey("passphrase.txt")).To(BeEmpty())

			v := SealedVolumeData{
				VolumeName:     "tofu-a1b2c3d4",
				PartitionLabel: "COS_PERSISTENT",
				SecretName:     "test-secret",
				SecretPath:     "pass_phrase",
			}
			_, path := v.DefaultSecret()
			Expect(path).To(Equal("pass_phrase"))

			v.SecretPath = "passphrase.txt"
			_, path = v.DefaultSecret()
			Expect(path).To(Equal("passphrase.txt"))
		})
	})

	Context("when the challenger generates the name itself", func() {
		It("builds a name Kubernetes accepts from an uppercase partition label", func() {
			v := SealedVolumeData{
				VolumeName:     "tofu-a1b2c3d4",
				PartitionLabel: "DATA",
			}
			name, path := v.DefaultSecret()
			Expect(validation.IsDNS1123Subdomain(name)).To(BeEmpty(),
				"generated name %q would be rejected by the API server", name)
			Expect(name).To(Equal("tofu-a1b2c3d4-data"))
			Expect(path).To(Equal("passphrase"))
		})

		It("keeps the name it already generated for the Kairos labels", func() {
			v := SealedVolumeData{
				VolumeName:     "tofu-a1b2c3d4",
				PartitionLabel: "COS_PERSISTENT",
			}
			name, path := v.DefaultSecret()
			Expect(name).To(Equal("tofu-a1b2c3d4-cos-persistent"))
			Expect(path).To(Equal("passphrase"))
		})

		It("defaults only the half the operator left empty", func() {
			v := SealedVolumeData{
				VolumeName:     "tofu-a1b2c3d4",
				PartitionLabel: "COS_PERSISTENT",
				SecretPath:     "pass_phrase",
			}
			name, path := v.DefaultSecret()
			Expect(name).To(Equal("tofu-a1b2c3d4-cos-persistent"))
			Expect(path).To(Equal("pass_phrase"))
		})
	})
})
