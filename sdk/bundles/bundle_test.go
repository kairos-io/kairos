package bundles_test

import (
	"debug/elf"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"

	. "github.com/kairos-io/kairos-sdk/bundles"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Bundle", func() {
	Context("install", func() {
		It("installs packages from luet repos", func() {
			dir, err := os.MkdirTemp("", "test")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			_ = os.MkdirAll(filepath.Join(dir, "var", "tmp", "luet"), os.ModePerm)
			err = RunBundles([]BundleOption{WithDBPath(dir), WithRootFS(dir), WithTarget("package://utils/nerdctl")})
			Expect(err).ToNot(HaveOccurred())
			Expect(filepath.Join(dir, "usr", "bin", "nerdctl")).To(BeARegularFile())
		})

		It("installs from container images", func() {
			dir, err := os.MkdirTemp("", "test")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(dir)
			err = RunBundles([]BundleOption{WithDBPath(dir), WithRootFS(dir), WithTarget("container://quay.io/kairos/packages:nerdctl-utils-2.1.5")})
			Expect(err).ToNot(HaveOccurred())
			binPath := filepath.Join(dir, "usr", "bin", "nerdctl")
			Expect(binPath).To(BeARegularFile())
			expectBinaryArch(binPath, runtime.GOARCH)
		})

		When("platform is specified", func() {
			for _, arch := range []string{"amd64", "arm64", "arm/v7"} {
				It(fmt.Sprintf("install with %s", arch), func() {
					dir, err := os.MkdirTemp("", "test")
					Expect(err).ToNot(HaveOccurred())
					defer os.RemoveAll(dir)
					err = RunBundles([]BundleOption{
						WithDBPath(dir),
						WithRootFS(dir),
						WithTarget("container://quay.io/luet/base:0.35.5"),
						WithPlatform(fmt.Sprintf("linux/%s", arch)),
					})
					Expect(err).ToNot(HaveOccurred())
					binPath := filepath.Join(dir, "usr", "bin", "luet")
					Expect(binPath).To(BeARegularFile())
					expectBinaryArch(binPath, arch)
				})
			}
		})

		When("local is true", func() {
			var installer BundleInstaller
			var config *BundleConfig
			var tmpDir, tmpFile string
			var err error

			BeforeEach(func() {
				config = &BundleConfig{
					LocalFile: true,
				}
			})

			AfterEach(func() {
				os.RemoveAll(tmpDir)
			})

			JustBeforeEach(func() {
				installer, err = NewBundleInstaller(*config)
				Expect(err).ToNot(HaveOccurred())
			})

			When("type is container", func() {
				BeforeEach(func() {
					tmpDir, err = os.MkdirTemp("", "test")
					Expect(err).ToNot(HaveOccurred())
					tmpFile = path.Join(tmpDir, "grub-config.tar")
					copyFile("../assets/grub-config.tar", tmpFile)

					config.Target = "container://" + tmpFile
					config.DBPath = "/usr/local/.kairos/db"
					config.RootPath = "/"
				})

				It("installs", func() {
					expectInstalled(installer, config)
				})
			})

			When("type is docker", func() {
				BeforeEach(func() {
					tmpDir, err = os.MkdirTemp("", "test")
					Expect(err).ToNot(HaveOccurred())
					tmpFile = path.Join(tmpDir, "grub-config.tar")
					copyFile("../assets/grub-config.tar", tmpFile)

					config.Target = "docker://" + tmpFile
					config.DBPath = "/usr/local/.kairos/db"
					config.RootPath = "/"
				})

				It("installs", func() {
					expectInstalled(installer, config)
				})
			})

			When("type is run", func() {
				BeforeEach(func() {
					// Ensure no leftovers from previous tests
					// These tests are meant to run in a container, so it should
					// be ok to delete files like this.
					os.RemoveAll("/var/lib/rancher/k3s/server/manifests/longhorn.yaml")

					tmpDir, err = os.MkdirTemp("", "test")
					Expect(err).ToNot(HaveOccurred())
					tmpFile = path.Join(tmpDir, "longhorn-bundle.tar")
					copyFile("../assets/longhorn-bundle.tar", tmpFile)
					config.Target = "run://" + tmpFile
				})

				It("installs", func() {
					_, err := os.Stat("/var/lib/rancher/k3s/server/manifests/longhorn.yaml")
					Expect(err).To(HaveOccurred())

					err = installer.Install(config)
					Expect(err).ToNot(HaveOccurred())
					_, err = os.Stat("/var/lib/rancher/k3s/server/manifests/longhorn.yaml")
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})

		When("local is false", func() {
			var installer BundleInstaller
			var config *BundleConfig
			var err error

			BeforeEach(func() {
				config = &BundleConfig{
					LocalFile: false,
				}
			})

			JustBeforeEach(func() {
				installer, err = NewBundleInstaller(*config)
				Expect(err).ToNot(HaveOccurred())
			})

			When("type is container", func() {
				BeforeEach(func() {
					config.Target = "container://quay.io/kairos/packages:grub-config-static-0.9"
					config.DBPath = "/usr/local/.kairos/db"
					config.RootPath = "/"
				})

				It("installs", func() {
					expectInstalled(installer, config)
				})
			})

			When("type is docker", func() {
				BeforeEach(func() {
					config.Target = "docker://quay.io/kairos/packages:grub-config-static-0.9"
					config.DBPath = "/usr/local/.kairos/db"
					config.RootPath = "/"
				})

				It("installs", func() {
					expectInstalled(installer, config)
				})
			})

			When("type is run", func() {
				BeforeEach(func() {
					os.RemoveAll("/var/lib/rancher/k3s/server/manifests/longhorn.yaml")
					config.Target = "run://quay.io/kairos/community-bundles:longhorn_latest"
				})

				It("installs", func() {
					_, err := os.Stat("/var/lib/rancher/k3s/server/manifests/longhorn.yaml")
					Expect(err).To(HaveOccurred())

					err = installer.Install(config)
					Expect(err).ToNot(HaveOccurred())
					_, err = os.Stat("/var/lib/rancher/k3s/server/manifests/longhorn.yaml")
					Expect(err).ToNot(HaveOccurred())
				})
			})
		})
	})
})

// Copied from: https://opensource.com/article/18/6/copying-files-go
func copyFile(src, dst string) {
	sourceFileStat, err := os.Stat(src)
	Expect(err).ToNot(HaveOccurred())
	Expect(sourceFileStat.Mode().IsRegular()).To(BeTrue())

	source, err := os.Open(src)
	Expect(err).ToNot(HaveOccurred())
	defer source.Close()

	destination, err := os.Create(dst)
	Expect(err).ToNot(HaveOccurred())
	defer destination.Close()

	_, err = io.Copy(destination, source)
	Expect(err).ToNot(HaveOccurred())
}

func expectInstalled(installer BundleInstaller, config *BundleConfig) {
	// Ensure no leftovers from previous tests
	// These tests are meant to run in a container, so it should
	// be ok to delete files like this.
	err := os.RemoveAll("/etc/cos/grub.cfg")
	Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat("/etc/cos/grub.cfg")
	Expect(err).To(HaveOccurred())

	err = installer.Install(config)
	Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat("/etc/cos/grub.cfg")
	Expect(err).ToNot(HaveOccurred())
}

func expectBinaryArch(path string, arch string) {
	f, err := elf.Open(path)
	Expect(err).ToNot(HaveOccurred())
	switch arch {
	case "amd64":
		Expect(f.Machine.String()).To(Equal(elf.EM_X86_64.String()))
	case "arm64":
		Expect(f.Machine.String()).To(Equal(elf.EM_AARCH64.String()))
	case "arm/v7":
		Expect(f.Machine.String()).To(Equal(elf.EM_ARM.String()))
	default:
		Fail(fmt.Sprintf("unsupported arch: %s", arch))
	}
}
