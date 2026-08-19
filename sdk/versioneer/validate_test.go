package versioneer_test

import (
	"github.com/kairos-io/kairos-sdk/versioneer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate", func() {
	var artifact versioneer.Artifact
	BeforeEach(func() {
		artifact = versioneer.Artifact{
			Flavor:        "opensuse",
			FlavorRelease: "leap-15.5",
			Variant:       "standard",
			Model:         "generic",
			Arch:          "amd64",
			Version:       "v2.4.2",
		}
	})

	When("artifact is valid", func() {
		It("returns nil", func() {
			Expect(artifact.Validate()).To(BeNil())
		})
	})

	It("returns an error when FlavorRelease is empty", func() {
		artifact.FlavorRelease = ""
		Expect(artifact.Validate()).To(MatchError("FlavorRelease is empty"))
	})

	It("returns an error when Variant is empty", func() {
		artifact.Variant = ""
		Expect(artifact.Validate()).To(MatchError("Variant is empty"))
	})

	It("returns an error when Model is empty", func() {
		artifact.Model = ""
		Expect(artifact.Validate()).To(MatchError("Model is empty"))
	})

	It("returns an error when Arch is empty", func() {
		artifact.Arch = ""
		Expect(artifact.Validate()).To(MatchError("Arch is empty"))
	})

	It("returns an error when SoftwareVersion is not empty but SoftwareVersionPrefix is empty", func() {
		artifact.SoftwareVersion = "v1.28.3"
		artifact.SoftwareVersionPrefix = ""

		Expect(artifact.Validate()).To(MatchError("SoftwareVersionPrefix should be defined when SoftwareVersion is not empty"))
	})
})
