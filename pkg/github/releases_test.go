package github_test

import (
	"context"
	"testing"

	"github.com/kairos-io/kairos/v2/pkg/github"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReleases(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Releases Suite")
}

var _ = Describe("Releases", func() {
	It("can find the proper releases in order", func() {
		releases, err := github.FindReleases(context.Background(), "", "kairos-io/kairos", false)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(releases)).To(BeNumerically(">", 0))
		// Expect the one at the bottom to be the first "real" release of kairos
		Expect(releases[len(releases)-1].Original()).To(Equal("v1.0.0"))
		// Expect the first one to be greater than the last one
		Expect(releases[0].GreaterThan(releases[len(releases)-1]))
	})
	It("can find the proper releases in order with prereleases", func() {
		releases, err := github.FindReleases(context.Background(), "", "kairos-io/kairos", true)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(releases)).To(BeNumerically(">", 0))
		// Expect the one at the bottom to be the first "real" release of kairos
		Expect(releases[len(releases)-1].Original()).To(Equal("v1.0.0"))
		// Expect the first one to be greater than the last one
		Expect(releases[0].GreaterThan(releases[len(releases)-1]))
	})
})
