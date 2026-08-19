package versioneer_test

import (
	"github.com/kairos-io/kairos-sdk/versioneer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BaseContainerName", func() {
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
	})

	When("no variant is passed", func() {
		var id, registryAndOrg string
		BeforeEach(func() {
			id = "master"
			registryAndOrg = "quay.io/kairos"
			artifact.Variant = ""
		})

		It("is valid", func() {
			_, err := artifact.BaseContainerName(registryAndOrg, id)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("artifact is valid", func() {
		var id, registryAndOrg string
		BeforeEach(func() {
			id = "master"
			registryAndOrg = "quay.io/kairos"
			expectedName = "quay.io/kairos/opensuse:leap-15.5-amd64-generic-master"
		})
		It("returns the name", func() {
			name, err := artifact.BaseContainerName(registryAndOrg, id)
			Expect(err).ToNot(HaveOccurred())
			Expect(name).To(Equal(expectedName))
		})
		When("no id is passed", func() {
			It("returns the name", func() {
				name, err := artifact.BaseContainerName(registryAndOrg, "")
				Expect(err).To(HaveOccurred(), name)
				Expect(err).To(MatchError("no id passed"))
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
