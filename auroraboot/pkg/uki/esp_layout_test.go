package uki

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The name of the systemd-boot binary on the ESP is decided twice: once by
// resolveSdBootFiles, which tells go-ukify where to write it, and once by
// imageFiles, which tells the image builder where to read it back from. When
// those two disagree the build fails late, after every UKI has already been
// signed, with a "no such file" about a path nobody asked for.
// kairos-io/kairos#5242.
var _ = Describe("ESP layout", func() {
	var sourceDir, keysDir string
	var entries []utils.BootEntry

	BeforeEach(func() {
		sourceDir = GinkgoT().TempDir()
		keysDir = GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(sourceDir, "entries"), 0755)).To(Succeed())
		entries = []utils.BootEntry{{FileName: "norole", Cmdline: "cos.setup", Title: "Kairos"}}
	})

	DescribeTable("imageFiles asks for the binary resolveSdBootFiles wrote",
		func(arch, expectedEfi, expectedStub, expectedSdBoot string) {
			// The producer side decides where go-ukify writes systemd-boot.
			stub, sdBoot, outEfi, err := resolveSdBootFiles(sourceDir, arch, false)
			Expect(err).ToNot(HaveOccurred())
			Expect(outEfi).To(Equal(expectedEfi))
			Expect(stub).To(Equal(expectedStub))
			Expect(sdBoot).To(Equal(expectedSdBoot))

			// The consumer side decides what goes into EFI/BOOT on the ESP.
			files, err := imageFiles(sourceDir, keysDir, entries, arch)
			Expect(err).ToNot(HaveOccurred())
			Expect(files["EFI/BOOT"]).To(ConsistOf(filepath.Join(sourceDir, outEfi)))
		},
		Entry("amd64", constants.ArchAmd64, constants.EfiFallbackNamex86,
			constants.UkiSystemdBootStubx86Path, constants.UkiSystemdBootx86Path),
		Entry("x86_64", constants.Archx86, constants.EfiFallbackNamex86,
			constants.UkiSystemdBootStubx86Path, constants.UkiSystemdBootx86Path),
		Entry("arm64", constants.ArchArm64, constants.EfiFallbackNameArm,
			constants.UkiSystemdBootStubArmPath, constants.UkiSystemdBootArmPath),
		Entry("aarch64", constants.Archaarch64, constants.EfiFallbackNameArm,
			constants.UkiSystemdBootStubArmPath, constants.UkiSystemdBootArmPath),
		Entry("riscv64", constants.ArchRiscv64, constants.EfiFallbackNameRiscv64,
			constants.UkiSystemdBootStubRiscv64Path, constants.UkiSystemdBootRiscv64Path),
	)

	DescribeTable("resolveSdBootFiles takes the binaries from the source rootfs",
		func(arch, stubName, sdBootName string) {
			Expect(os.WriteFile(filepath.Join(sourceDir, stubName), []byte("stub"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(sourceDir, sdBootName), []byte("sdboot"), 0644)).To(Succeed())

			stub, sdBoot, _, err := resolveSdBootFiles(sourceDir, arch, true)
			Expect(err).ToNot(HaveOccurred())
			Expect(stub).To(Equal(filepath.Join(sourceDir, stubName)))
			Expect(sdBoot).To(Equal(filepath.Join(sourceDir, sdBootName)))
		},
		Entry("amd64", constants.ArchAmd64, constants.UkiSystemdBootStubx86Name, constants.UkiSystemdBootx86Name),
		Entry("arm64", constants.ArchArm64, constants.UkiSystemdBootStubArmName, constants.UkiSystemdBootArmName),
		Entry("riscv64", constants.ArchRiscv64, constants.UkiSystemdBootStubRiscv64Name, constants.UkiSystemdBootRiscv64Name),
	)

	It("reports the systemd-boot binary the source rootfs does not carry", func() {
		Expect(os.WriteFile(filepath.Join(sourceDir, constants.UkiSystemdBootStubRiscv64Name), []byte("stub"), 0644)).To(Succeed())

		_, _, _, err := resolveSdBootFiles(sourceDir, constants.ArchRiscv64, true)
		Expect(err).To(MatchError(ContainSubstring("finding systemd-boot in source")))
		Expect(err).To(MatchError(ContainSubstring(constants.UkiSystemdBootRiscv64Name)))
	})

	It("lists the auto-enroll keys from the public keys dir", func() {
		files, err := imageFiles(sourceDir, keysDir, entries, constants.ArchAmd64)
		Expect(err).ToNot(HaveOccurred())
		Expect(files).To(HaveKey("loader/keys"))
		Expect(files["loader/keys/auto"]).To(ConsistOf(
			filepath.Join(keysDir, "PK.auth"),
			filepath.Join(keysDir, "KEK.auth"),
			filepath.Join(keysDir, "db.auth"),
		))
	})

	It("leaves out the auto-enroll keys when there is no public keys dir", func() {
		files, err := imageFiles(sourceDir, "", entries, constants.ArchAmd64)
		Expect(err).ToNot(HaveOccurred())
		Expect(files).To(HaveKey("loader/keys"))
		Expect(files).ToNot(HaveKey("loader/keys/auto"))
		for dir, list := range files {
			for _, f := range list {
				Expect(filepath.IsAbs(f)).To(BeTrue(), "%s lists relative path %s", dir, f)
			}
		}
	})

	It("refuses an architecture it has no fallback binary for", func() {
		_, err := imageFiles(sourceDir, keysDir, entries, "ppc64le")
		Expect(err).To(MatchError(ContainSubstring("unsupported arch: ppc64le")))

		_, _, _, err = resolveSdBootFiles(sourceDir, "ppc64le", false)
		Expect(err).To(MatchError(ContainSubstring("unsupported arch: ppc64le")))
	})
})
