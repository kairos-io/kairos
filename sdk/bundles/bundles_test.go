package bundles_test

import (
	"github.com/kairos-io/kairos/v4/sdk/bundles"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NewBundleInstaller", func() {
	newFor := func(target string) (bundles.BundleInstaller, error) {
		return bundles.NewBundleInstaller(bundles.BundleConfig{Target: target})
	}

	DescribeTable("selects the installer the scheme names",
		func(target string, expected interface{}) {
			installer, err := newFor(target)
			Expect(err).ToNot(HaveOccurred())
			Expect(installer).To(BeAssignableToTypeOf(expected))
		},
		Entry("container", "container://quay.io/kairos/packages:some-bundle", &bundles.OCIImageExtractor{}),
		Entry("docker", "docker://quay.io/kairos/packages:some-bundle", &bundles.OCIImageExtractor{}),
		Entry("uppercase docker", "DOCKER://quay.io/kairos/packages:some-bundle", &bundles.OCIImageExtractor{}),
		Entry("run", "run://quay.io/kairos/packages:some-bundle", &bundles.OCIImageRunner{}),
		Entry("package", "package://utils/edgevpn", &bundles.LuetInstaller{}),
	)

	DescribeTable("refuses a scheme it cannot install",
		func(target string) {
			installer, err := newFor(target)
			Expect(err).To(HaveOccurred())
			Expect(installer).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(target))
			for _, scheme := range bundles.SupportedTargetSchemes {
				Expect(err.Error()).To(ContainSubstring(scheme))
			}
		},
		Entry("oci", "oci://quay.io/kairos/packages:some-bundle"),
		Entry("https", "https://example.com/bundle.tar"),
		Entry("a typo", "contaner://quay.io/kairos/packages:some-bundle"),
	)

	It("still refuses a target with no scheme at all", func() {
		installer, err := newFor("quay.io/kairos/packages:some-bundle")
		Expect(err).To(MatchError("invalid target"))
		Expect(installer).To(BeNil())
	})
})

var _ = Describe("RunBundles", func() {
	It("reports the unsupported scheme instead of shelling out to luet", func() {
		err := bundles.RunBundles([]bundles.BundleOption{
			bundles.WithTarget("oci://quay.io/kairos/packages:some-bundle"),
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported bundle target scheme"))
		Expect(err.Error()).ToNot(ContainSubstring("luet"))
	})
})
