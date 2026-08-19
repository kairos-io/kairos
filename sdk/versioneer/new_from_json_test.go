package versioneer_test

import (
	"github.com/kairos-io/kairos-sdk/versioneer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewArtifactFromJSON", func() {
	It("returns an object from the given json", func() {
		jsonStr := `{
		  "flavor":"opensuse-leap",
			"flavorRelease":"15.5",
			"family":"opensuse",
			"variant":"standard",
			"model":"generic",
			"arch":"amd64",
			"version":"v2.4.2",
			"softwareVersion":"k3sv1.26.9+k3s1"
		}`

		artifact, err := versioneer.NewArtifactFromJSON(jsonStr)

		Expect(err).ToNot(HaveOccurred())
		Expect(artifact.Flavor).To(Equal("opensuse-leap"))
		Expect(artifact.Family).To(Equal("opensuse"))
		Expect(artifact.FlavorRelease).To(Equal("15.5"))
		Expect(artifact.Variant).To(Equal("standard"))
		Expect(artifact.Model).To(Equal("generic"))
		Expect(artifact.Arch).To(Equal("amd64"))
		Expect(artifact.Version).To(Equal("v2.4.2"))
		Expect(artifact.SoftwareVersion).To(Equal("k3sv1.26.9+k3s1"))
	})
})
