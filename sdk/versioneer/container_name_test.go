package versioneer_test

import (
	"github.com/kairos-io/kairos-sdk/versioneer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ContainerName", func() {
	var artifact versioneer.Artifact
	var expectedName string
	var registryAndOrg string

	BeforeEach(func() {
		artifact = versioneer.Artifact{
			Flavor:                "opensuse",
			FlavorRelease:         "leap-15.5",
			Variant:               "standard",
			Model:                 "generic",
			Arch:                  "amd64",
			Version:               "v2.4.2",
			SoftwareVersion:       "v1.26.9+k3s1",
			SoftwareVersionPrefix: "k3s",
		}

		registryAndOrg = "quay.io/kairos"
	})

	When("artifact is valid", func() {
		When("SoftwareVersion is empty", func() {
			BeforeEach(func() {
				artifact.SoftwareVersion = ""
				expectedName = "quay.io/kairos/opensuse:leap-15.5-standard-amd64-generic-v2.4.2"
			})

			It("returns the name", func() {
				name, err := artifact.ContainerName(registryAndOrg)
				Expect(err).ToNot(HaveOccurred())
				Expect(name).To(Equal(expectedName))
			})
		})

		When("SoftwareVersion is not empty", func() {
			BeforeEach(func() {
				expectedName = "quay.io/kairos/opensuse:leap-15.5-standard-amd64-generic-v2.4.2-k3sv1.26.9-k3s1"
			})
			It("returns the name", func() {
				name, err := artifact.ContainerName(registryAndOrg)
				Expect(err).ToNot(HaveOccurred())
				Expect(name).To(Equal(expectedName))
			})
		})
	})

	When("artifact is invalid", func() {
		BeforeEach(func() {
			artifact.Flavor = ""
		})
		It("returns an error", func() {
			_, err := artifact.ContainerName(registryAndOrg)
			Expect(err).To(MatchError("Flavor is empty"))
		})
	})
})
