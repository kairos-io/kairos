package versioneer_test

import (
	"sort"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/versioneer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OSReleaseVariables", func() {
	var artifact versioneer.Artifact

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

	It("returns the same output on every call", func() {
		first, err := artifact.OSReleaseVariables("quay.io/kairos", "kairos-io/kairos", "https://bug", "https://home")
		Expect(err).ToNot(HaveOccurred())

		for i := 0; i < 50; i++ {
			again, err := artifact.OSReleaseVariables("quay.io/kairos", "kairos-io/kairos", "https://bug", "https://home")
			Expect(err).ToNot(HaveOccurred())
			Expect(again).To(Equal(first))
		}
	})

	It("lists the variables sorted by name", func() {
		out, err := artifact.OSReleaseVariables("quay.io/kairos", "kairos-io/kairos", "https://bug", "https://home")
		Expect(err).ToNot(HaveOccurred())

		lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		names := make([]string, 0, len(lines))
		for _, l := range lines {
			names = append(names, strings.SplitN(l, "=", 2)[0])
		}
		Expect(sort.StringsAreSorted(names)).To(BeTrue(), "got order: %v", names)
	})
})
