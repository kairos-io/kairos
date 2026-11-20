package testartifacts

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTestartifacts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Testartifacts Suite")
}

var _ = Describe("AuroraBoot from the same commit", func() {
	Describe("AuroraBootBinary", func() {
		It("uses the binary named by KAIROS_TEST_AURORABOOT_BINARY as is", func() {
			GinkgoT().Setenv(binaryEnv, "/opt/ci/auroraboot")
			path, err := AuroraBootBinary(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal("/opt/ci/auroraboot"))
		})
	})

	Describe("AuroraBootImage", func() {
		It("uses the image named by KAIROS_TEST_AURORABOOT_IMAGE as is", func() {
			GinkgoT().Setenv(imageEnv, "auroraboot:ci")
			image, err := AuroraBootImage(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(image).To(Equal("auroraboot:ci"))
		})
	})

	Describe("sourceDir", func() {
		It("finds the auroraboot module that sits next to this package", func() {
			dir, err := sourceDir()
			Expect(err).NotTo(HaveOccurred())
			Expect(filepath.Join(dir, "go.mod")).To(BeARegularFile())
			data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("module github.com/kairos-io/kairos/auroraboot"))
		})
	})
})
