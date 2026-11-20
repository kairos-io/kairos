package auroraboot

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/auroraboot/pkg/builder"
)

var _ = Describe("KairosInitImageFor", func() {
	DescribeTable("picks the kairos-init tag that matches the AuroraBoot version",
		func(version, want string) {
			Expect(KairosInitImageFor(version)).To(Equal("quay.io/kairos/kairos-init:" + want))
		},
		Entry("a release", "v4.5.1", "v4.5.1"),
		Entry("a release candidate", "v4.6.0-rc2", "v4.6.0-rc2"),
		Entry("a dotted prerelease", "v4.6.0-beta.1", "v4.6.0-beta.1"),
		Entry("the placeholder of an unversioned build", "v0.0.0", "latest"),
		Entry("an empty version", "", "latest"),
		Entry("a development build", "dev", "latest"),
		Entry("a build between tags", "v4.5.1-5-gabc1234", "latest"),
		Entry("a dirty build", "v4.5.1-dirty", "latest"),
		Entry("a version without the v", "4.5.1", "latest"),
	)
})

var _ = Describe("WithKairosInitImage", func() {
	It("keeps the default for an empty reference", func() {
		b, err := New(GinkgoT().TempDir(), nil, nil).WithKairosInitImage("")
		Expect(err).NotTo(HaveOccurred())
		Expect(b.kairosInitImage).To(Equal(KairosInitImageFor("")))
	})

	It("refuses a reference that would add lines to the kairosify Dockerfile", func() {
		b, err := New(GinkgoT().TempDir(), nil, nil).WithKairosInitImage("quay.io/kairos/kairos-init:v0.5.0\nRUN id")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("kairos-init image"))
		Expect(b).To(BeNil())
	})
})

var _ = Describe("kairosify kairos-init image", func() {
	var outputDir string

	// A cancelled context stops the docker build from starting, which leaves
	// the rendered Dockerfile on disk for inspection.
	render := func(b *Builder, opts builder.BuildOptions) string {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := b.kairosify(ctx, "ubuntu:24.04", opts, outputDir, nil)
		Expect(err).To(HaveOccurred())
		data, err := os.ReadFile(filepath.Join(outputDir, "Dockerfile.kairosify"))
		Expect(err).NotTo(HaveOccurred())
		return string(data)
	}

	BeforeEach(func() {
		outputDir = GinkgoT().TempDir()
	})

	It("uses the default when nothing else names one", func() {
		b := New(outputDir, nil, &kairosifyTestStore{})
		Expect(render(b, builder.BuildOptions{ID: "a-1"})).To(HavePrefix(
			"FROM " + KairosInitImageFor("") + " AS kairos-init\n"))
	})

	It("uses the image the builder was configured with", func() {
		b, err := New(outputDir, nil, &kairosifyTestStore{}).WithKairosInitImage("localhost:5000/kairos/kairos-init:dev")
		Expect(err).NotTo(HaveOccurred())
		Expect(render(b, builder.BuildOptions{ID: "a-1"})).To(HavePrefix(
			"FROM localhost:5000/kairos/kairos-init:dev AS kairos-init\n"))
	})

	It("prefers the image the build request names", func() {
		b, err := New(outputDir, nil, &kairosifyTestStore{}).WithKairosInitImage("localhost:5000/kairos/kairos-init:dev")
		Expect(err).NotTo(HaveOccurred())
		Expect(render(b, builder.BuildOptions{ID: "a-1", KairosInitImage: "quay.io/kairos/kairos-init:v0.5.0"})).To(HavePrefix(
			"FROM quay.io/kairos/kairos-init:v0.5.0 AS kairos-init\n"))
	})
})
