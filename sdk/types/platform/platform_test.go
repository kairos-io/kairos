package platform_test

import (
	"fmt"

	sdkPlatform "github.com/kairos-io/kairos-sdk/types/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("Types", Label("types", "common"), func() {
	Describe("Platform", func() {
		It("Returns the platform", func() {
			platform, err := sdkPlatform.NewPlatform("linux", sdkPlatform.ArchArm64)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("linux"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))

			platform, err = sdkPlatform.NewPlatform("linux", sdkPlatform.ArchRiscv)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("linux"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchRiscv))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchRiscv))

			platform, err = sdkPlatform.NewPlatform("linux", sdkPlatform.Archx86)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("linux"))
			Expect(platform.Arch).To(Equal(sdkPlatform.Archx86))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchAmd64))
		})
		It("Does not check the validity of the os", func() {
			platform, err := sdkPlatform.NewPlatform("jojo", sdkPlatform.ArchArm64)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("jojo"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))
		})
		It("Does check the validity of the arch", func() {
			_, err := sdkPlatform.NewPlatform("jojo", "what")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid arch"))
		})
		It("Returns the platform from a source arch", func() {
			platform, err := sdkPlatform.NewPlatformFromArch(sdkPlatform.ArchAmd64)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.Arch).To(Equal(sdkPlatform.Archx86))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchAmd64))

			platform, err = sdkPlatform.NewPlatformFromArch(sdkPlatform.ArchArm64)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))

			platform, err = sdkPlatform.NewPlatformFromArch(sdkPlatform.ArchRiscv)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchRiscv))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchRiscv))

			platform, err = sdkPlatform.NewPlatformFromArch(sdkPlatform.Archx86)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.Arch).To(Equal(sdkPlatform.Archx86))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchAmd64))

		})
		It("Parses the platform from a string", func() {
			platform, err := sdkPlatform.ParsePlatform(fmt.Sprintf("jojo/%s", sdkPlatform.ArchArm64))
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("jojo"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))

		})
		It("Has a proper string representation", func() {
			platform, err := sdkPlatform.NewPlatform("jojo", sdkPlatform.ArchArm64)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("jojo"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.String()).To(Equal(fmt.Sprintf("jojo/%s", sdkPlatform.ArchArm64)))
		})
		It("Marshals and unmarshalls correctly", func() {
			// CustomUnmarshall
			platform := sdkPlatform.Platform{}
			// This should update the object properly
			_, err := platform.CustomUnmarshal("linux/arm64")
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("linux"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.String()).To(Equal(fmt.Sprintf("linux/%s", sdkPlatform.ArchArm64)))

			// Marshall
			y, err := platform.MarshalYAML()
			Expect(err).ToNot(HaveOccurred())
			Expect(y).To(Equal(fmt.Sprintf("linux/%s", sdkPlatform.ArchArm64)))

			// Unmarshall
			platform = sdkPlatform.Platform{}
			// Check that its empty
			Expect(platform.OS).To(Equal(""))
			Expect(platform.Arch).To(Equal(""))
			Expect(platform.GolangArch).To(Equal(""))
			node := &yaml.Node{Value: fmt.Sprintf("linux/%s", sdkPlatform.ArchArm64)}
			// This should update the object properly with the yaml node
			err = platform.UnmarshalYAML(node)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform.OS).To(Equal("linux"))
			Expect(platform.Arch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.GolangArch).To(Equal(sdkPlatform.ArchArm64))
			Expect(platform.String()).To(Equal(fmt.Sprintf("linux/%s", sdkPlatform.ArchArm64)))

		})
	})
})
