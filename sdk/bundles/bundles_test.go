package bundles_test

import (
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/bundles"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBundles(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bundles Suite")
}

// localTarget builds the options for a container bundle read from a local
// tarball that does not exist. The install fails in tarball.ImageFromPath
// without touching the network, which is what makes these specs runnable
// anywhere.
func localTarget(root, name string) []bundles.BundleOption {
	return []bundles.BundleOption{
		bundles.WithTarget("container://" + filepath.Join(root, name)),
		bundles.WithLocalFile(true),
		bundles.WithRootFS(root),
	}
}

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
	var root string

	BeforeEach(func() {
		root = GinkgoT().TempDir()
	})

	It("reports every bundle that failed, not only the last one", func() {
		err := bundles.RunBundles(
			localTarget(root, "first.tar"),
			localTarget(root, "second.tar"),
			localTarget(root, "third.tar"),
		)
		Expect(err).To(HaveOccurred())

		// Before the fix each iteration replaced the accumulator, so three
		// failing bundles produced a single-entry error naming only "third".
		Expect(err.Error()).To(ContainSubstring("3 errors occurred"))
		Expect(err.Error()).To(ContainSubstring("first.tar"))
		Expect(err.Error()).To(ContainSubstring("second.tar"))
		Expect(err.Error()).To(ContainSubstring("third.tar"))
	})

	It("names the target that failed", func() {
		err := bundles.RunBundles(localTarget(root, "only.tar"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(filepath.Join(root, "only.tar")))
	})

	It("returns nil when there is nothing to install", func() {
		Expect(bundles.RunBundles()).To(BeNil())
	})

	It("rejects a target with no scheme", func() {
		err := bundles.RunBundles([]bundles.BundleOption{bundles.WithTarget("no-scheme")})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid target"))
	})

	It("reports the unsupported scheme instead of shelling out to luet", func() {
		err := bundles.RunBundles([]bundles.BundleOption{
			bundles.WithTarget("oci://quay.io/kairos/packages:some-bundle"),
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported bundle target scheme"))
		Expect(err.Error()).ToNot(ContainSubstring("luet"))
	})
})
