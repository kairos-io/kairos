package bundles_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/c3os-io/c3os/sdk/bundles"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bundle", func() {
	Context("install", func() {
		PIt("installs packages from luet repos", func() {
			dir, err := ioutil.TempDir("", "test")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			os.MkdirAll(filepath.Join(dir, "var", "tmp", "luet"), os.ModePerm)
			err = RunBundles([]BundleOption{WithDBPath(dir), WithRootFS(dir), WithTarget("package://utils/edgevpn")})
			Expect(err).ToNot(HaveOccurred())
			Expect(filepath.Join(dir, "usr", "bin", "edgevpn")).To(BeARegularFile())
		})

		It("installs from container images", func() {
			dir, err := ioutil.TempDir("", "test")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			err = RunBundles([]BundleOption{WithDBPath(dir), WithRootFS(dir), WithTarget("container://quay.io/mocaccino/extra:edgevpn-utils-0.15.0")})
			Expect(err).ToNot(HaveOccurred())
			Expect(filepath.Join(dir, "usr", "bin", "edgevpn")).To(BeARegularFile())
		})
	})
})
