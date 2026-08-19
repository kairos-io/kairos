package versioneer_test

import (
	"os"

	"github.com/kairos-io/kairos-sdk/versioneer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewArtifactFromOSRelease", func() {
	var tmpOSReleaseFile *os.File
	var err error
	var osReleaseContent string

	BeforeEach(func() {
		tmpOSReleaseFile, err = os.CreateTemp("", "kairos-release")
		Expect(err).ToNot(HaveOccurred())

		osReleaseContent = "KAIROS_FLAVOR=opensuse\n" +
			"KAIROS_FLAVOR_RELEASE=leap-15.5\n" +
			"KAIROS_VARIANT=standard\n" +
			"KAIROS_FAMILY=opensuse\n" +
			"KAIROS_TARGETARCH=amd64\n" +
			"KAIROS_MODEL=generic\n" +
			"KAIROS_RELEASE=v2.4.2\n" +
			"KAIROS_SOFTWARE_VERSION=v1.26.9+k3s1\n" +
			"KAIROS_SOFTWARE_VERSION_PREFIX=k3s\n"

		err = os.WriteFile(tmpOSReleaseFile.Name(), []byte(osReleaseContent), 0644)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		os.Remove(tmpOSReleaseFile.Name())
	})

	It("builds a correct object", func() {
		artifact, err := versioneer.NewArtifactFromOSRelease(tmpOSReleaseFile.Name())

		Expect(err).ToNot(HaveOccurred())
		Expect(artifact.Flavor).To(Equal("opensuse"))
		Expect(artifact.Family).To(Equal("opensuse"))
		Expect(artifact.FlavorRelease).To(Equal("leap-15.5"))
		Expect(artifact.Variant).To(Equal("standard"))
		Expect(artifact.Model).To(Equal("generic"))
		Expect(artifact.Arch).To(Equal("amd64"))
		Expect(artifact.Version).To(Equal("v2.4.2"))
		Expect(artifact.SoftwareVersion).To(Equal("v1.26.9+k3s1"))
		Expect(artifact.SoftwareVersionPrefix).To(Equal("k3s"))
		Expect(artifact.Validate()).ToNot(HaveOccurred())
	})
})
