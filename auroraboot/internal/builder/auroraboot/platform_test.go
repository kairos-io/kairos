package auroraboot

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// buildxInspectOutput is the shape of a real "docker buildx inspect" reply on a
// host that has qemu-user-static registered for the foreign architectures.
const buildxInspectOutput = `Name:          default
Driver:        docker-container
Last Activity: 2026-09-07 10:11:12 +0000 UTC

Nodes:
Name:      default0
Endpoint:  unix:///var/run/docker.sock
Status:    running
Platforms: linux/arm64*, linux/amd64, linux/arm/v7, linux/386
`

var _ = Describe("Build platform preflight", func() {
	// withPlatforms builds a Builder whose probe answers with a fixed list, so
	// no spec here consults the developer's own docker.
	withPlatforms := func(platforms []string, err error) *Builder {
		return &Builder{platformsFn: func(context.Context) ([]string, error) {
			return platforms, err
		}}
	}

	It("parses the platform list, dropping the preferred marker", func() {
		Expect(parseBuildxPlatforms(buildxInspectOutput)).To(Equal([]string{
			"linux/arm64", "linux/amd64", "linux/arm/v7", "linux/386",
		}))
	})

	It("reads every node's platforms", func() {
		out := "Platforms: linux/arm64*\nPlatforms: linux/amd64\n"
		Expect(parseBuildxPlatforms(out)).To(Equal([]string{"linux/arm64", "linux/amd64"}))
	})

	It("finds no platforms in unrelated output", func() {
		Expect(parseBuildxPlatforms("Name: default\nDriver: docker\n")).To(BeEmpty())
	})

	It("accepts an architecture the builder can emulate", func() {
		b := withPlatforms([]string{"linux/arm64", "linux/amd64"}, nil)
		Expect(b.checkBuildPlatform(context.Background(), "amd64")).To(Succeed())
	})

	It("accepts an architecture the builder offers only as a variant", func() {
		b := withPlatforms([]string{"linux/arm64", "linux/arm/v7"}, nil)
		Expect(b.checkBuildPlatform(context.Background(), "arm")).To(Succeed())
	})

	// The API accepts either spelling (README.md lists both), and
	// pkg/handlers/artifacts.go passes the request's arch straight through.
	// buildx only ever reports GOARCH names, so comparing the request verbatim
	// refuses a host that can build it.
	DescribeTable("accepts the alternative spelling of an architecture",
		func(arch string, offered []string) {
			b := withPlatforms(offered, nil)
			Expect(b.checkBuildPlatform(context.Background(), arch)).To(Succeed())
		},
		Entry("x86_64 against a builder offering linux/amd64", "x86_64", []string{"linux/amd64"}),
		Entry("aarch64 against a builder offering linux/arm64", "aarch64", []string{"linux/arm64"}),
	)

	It("rejects an architecture the builder cannot run, and says how to fix it", func() {
		b := withPlatforms([]string{"linux/arm64"}, nil)
		err := b.checkBuildPlatform(context.Background(), "amd64")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(And(
			ContainSubstring("cannot build for linux/amd64"),
			ContainSubstring("it offers linux/arm64"),
			ContainSubstring("binfmt_misc"),
			ContainSubstring("tonistiigi/binfmt --install amd64"),
		))
	})

	// The instruction has to name an architecture binfmt can actually install,
	// or the user is told to run a command that changes nothing.
	It("names the GOARCH spelling in the refusal, whichever spelling was asked for", func() {
		b := withPlatforms([]string{"linux/arm64"}, nil)
		err := b.checkBuildPlatform(context.Background(), "x86_64")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(And(
			ContainSubstring("cannot build for linux/amd64"),
			ContainSubstring("tonistiigi/binfmt --install amd64"),
		))
		Expect(err.Error()).NotTo(ContainSubstring("x86_64"))
	})

	It("skips the check when no architecture was requested", func() {
		b := withPlatforms(nil, errors.New("buildx must not be consulted"))
		Expect(b.checkBuildPlatform(context.Background(), "")).To(Succeed())
	})

	It("allows the build when buildx cannot be probed", func() {
		b := withPlatforms(nil, errors.New("docker buildx inspect: executable file not found"))
		Expect(b.checkBuildPlatform(context.Background(), "amd64")).To(Succeed())
	})

	It("allows the build when buildx reports no platforms at all", func() {
		b := withPlatforms(nil, nil)
		Expect(b.checkBuildPlatform(context.Background(), "amd64")).To(Succeed())
	})

	It("defaults to the real buildx probe when nothing was injected", func() {
		Expect(New("", nil, nil).platformsFn).NotTo(BeNil())
	})
})
