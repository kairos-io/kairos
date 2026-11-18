package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

var _ = Describe("ISO image generation", Label("iso", "e2e"), func() {
	Context("build", func() {
		var tempDir string
		var err error
		var aurora *Auroraboot
		BeforeEach(func() {
			format.MaxLength = 0
			tempDir, err = os.MkdirTemp("", "auroraboot-test-")
			Expect(err).ToNot(HaveOccurred())

			err = WriteConfig("test", tempDir)
			Expect(err).ToNot(HaveOccurred())
			aurora = NewAuroraboot()
			// Map the config.yaml file to the container and the temp dir to the state dir
			aurora.ManualDirs = map[string]string{
				fmt.Sprintf("%s/config.yaml", tempDir): "/config.yaml",
				tempDir:                                "/tmp/auroraboot",
			}
		})

		AfterEach(func() {
			os.RemoveAll(tempDir)
		})

		It("generate an iso image from a container", func() {
			image := "quay.io/kairos/rockylinux:9-core-amd64-generic-v3.3.1"
			_, err := PullImage(image)
			Expect(err).ToNot(HaveOccurred())

			out, err := aurora.Run("--debug",
				"--set", fmt.Sprintf("container_image=oci://%s", image),
				"--set", "disable_http_server=true",
				"--set", "disable_netboot=true",
				"--set", "state_dir=/tmp/auroraboot",
				"--cloud-config", "/config.yaml",
			)
			Expect(out).To(ContainSubstring("Generating iso"), out)
			Expect(out).To(ContainSubstring("gen-iso"), out)
			Expect(out).ToNot(ContainSubstring("build-arm-image"), out)
			Expect(err).ToNot(HaveOccurred())
			_, err = os.Stat(filepath.Join(tempDir, "kairos-rockylinux-9-core-amd64-generic-v3.3.1.iso"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("fails if cloud config is empty", func() {
			image := "quay.io/kairos/rockylinux:9-core-amd64-generic-v3.3.1"

			err := WriteConfig("", tempDir)
			Expect(err).ToNot(HaveOccurred())

			out, err := aurora.Run(
				"--set", fmt.Sprintf("container_image=oci://%s", image),
				"--set", "disable_http_server=true",
				"--set", "disable_netboot=true",
				"--cloud-config", "/config.yaml")
			Expect(err).To(HaveOccurred(), out)
			Expect(out).To(MatchRegexp("cloud config set but contents are empty"))
		})

		It("generate an iso image from a release", func() {
			out, err := aurora.Run("--debug",
				"--set", "disable_http_server=true",
				"--set", "artifact_version=v3.3.1",
				"--set", "release_version=v3.3.1",
				"--set", "flavor=rockylinux",
				"--set", "flavor_release=9",
				"--set", "repository=kairos-io/kairos",
				"--set", "disable_netboot=true",
				"--set", "state_dir=/tmp/auroraboot",
				"--cloud-config", "/config.yaml",
			)
			Expect(out).To(ContainSubstring("Adding cloud config file"), out)
			Expect(out).ToNot(ContainSubstring("gen-iso"), out)
			Expect(out).To(ContainSubstring("download-iso"), out)
			Expect(out).To(ContainSubstring("inject-cloud-config"), out)
			Expect(out).ToNot(ContainSubstring("build-arm-image"), out)
			Expect(err).ToNot(HaveOccurred())
			_, err = os.Stat(filepath.Join(tempDir, "kairos.iso"))
			Expect(err).ToNot(HaveOccurred())
		})

		It("fails when --arch arm64 is used with an amd64-only image", func() {
			image := "quay.io/kairos/ubuntu:22.04-core-amd64-generic-v3.6.1-beta2"
			out, err := aurora.Run("build-iso",
				"--arch", "arm64",
				"--output", "/tmp/auroraboot",
				"--cloud-config", "/config.yaml",
				fmt.Sprintf("oci://%s", image),
			)
			Expect(err).To(HaveOccurred(), out)
			// The error should indicate that the arm64 layer cannot be found
			Expect(out).To(Or(
				ContainSubstring("arm64"),
				ContainSubstring("no matching manifest"),
				ContainSubstring("platform"),
				ContainSubstring("architecture"),
			), out)
		})

		It("succeeds when --arch arm64 is used with an arm64 image", func() {
			image := "quay.io/kairos/ubuntu:22.04-core-arm64-rpi4-v3.6.1-beta2"

			out, err := aurora.Run("build-iso",
				"--arch", "arm64",
				"--output", "/tmp/auroraboot",
				"--cloud-config", "/config.yaml",
				fmt.Sprintf("oci://%s", image),
			)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("Generating iso"), out)
			// Verify that an ISO file was created
			// The ISO name should match the image name pattern
			files, err := filepath.Glob(filepath.Join(tempDir, "*.iso"))
			Expect(err).ToNot(HaveOccurred())
			Expect(len(files)).To(BeNumerically(">", 0), "Expected at least one ISO file to be created")

			// build-iso keeps its helper files in a private dir: the config the
			// user mounted stays as it was, and no netboot dir is left behind.
			cc, err := os.ReadFile(filepath.Join(tempDir, "config.yaml"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(cc)).To(Equal("test"))
			Expect(filepath.Join(tempDir, "netboot")).ToNot(BeADirectory())
		})
	})

	Context("build-iso output directory", func() {
		const image = "quay.io/kairos/rockylinux:9-core-amd64-generic-v3.3.1"

		var outDir, ccDir string
		var aurora *Auroraboot

		// listDir returns the sorted entry names of dir.
		listDir := func(dir string) []string {
			entries, err := os.ReadDir(dir)
			Expect(err).ToNot(HaveOccurred())
			names := []string{}
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return names
		}

		// isoConfig reads /config.yaml out of an ISO inside the container.
		isoConfig := func(iso string) string {
			out, err := aurora.ContainerRun("sh", "-c", fmt.Sprintf(
				"xorriso -osirrox on -indev %s -extract /config.yaml /tmp/c >/dev/null 2>&1 && cat /tmp/c", iso))
			Expect(err).ToNot(HaveOccurred(), out)
			return out
		}

		BeforeEach(func() {
			format.MaxLength = 0
			var err error
			outDir, err = os.MkdirTemp("", "auroraboot-out-")
			Expect(err).ToNot(HaveOccurred())
			ccDir, err = os.MkdirTemp("", "auroraboot-cc-")
			Expect(err).ToNot(HaveOccurred())

			aurora = NewAuroraboot(outDir, ccDir)

			_, err = PullImage(image)
			Expect(err).ToNot(HaveOccurred())

			// The container runs as root, so the ISOs it writes are root owned.
			// Remove them from inside a container.
			DeferCleanup(func() {
				_, _ = aurora.ContainerRun("sh", "-c", fmt.Sprintf("rm -rf %s/* %s/*", outDir, ccDir))
				os.RemoveAll(outDir)
				os.RemoveAll(ccDir)
			})
		})

		It("keeps --output to the ISOs when two builds share it", func() {
			user := []byte("hostname: users-own\n")
			Expect(os.WriteFile(filepath.Join(outDir, "config.yaml"), user, 0o600)).To(Succeed())
			for _, n := range []string{"a", "b"} {
				Expect(os.WriteFile(filepath.Join(ccDir, n+".yaml"), []byte("#cloud-config\nhostname: build-"+n+"\n"), 0o600)).To(Succeed())
			}

			outs := map[string]string{}
			errs := map[string]error{}
			var mu sync.Mutex
			var wg sync.WaitGroup
			for _, n := range []string{"a", "b"} {
				wg.Add(1)
				go func(n string) {
					defer GinkgoRecover()
					defer wg.Done()
					out, err := aurora.Run("build-iso",
						"--output", outDir,
						"-n", n,
						"--cloud-config", filepath.Join(ccDir, n+".yaml"),
						"oci://"+image,
					)
					mu.Lock()
					defer mu.Unlock()
					outs[n], errs[n] = out, err
				}(n)
			}
			wg.Wait()

			for _, n := range []string{"a", "b"} {
				Expect(errs[n]).ToNot(HaveOccurred(), outs[n])
			}
			Expect(listDir(outDir)).To(ConsistOf("config.yaml", "a.iso", "a.iso.sha256", "b.iso", "b.iso.sha256"))
			got, err := os.ReadFile(filepath.Join(outDir, "config.yaml"))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(user))

			Expect(isoConfig(filepath.Join(outDir, "a.iso"))).To(ContainSubstring("hostname: build-a"))
			Expect(isoConfig(filepath.Join(outDir, "b.iso"))).To(ContainSubstring("hostname: build-b"))
		})

		It("writes to the working directory when --output is omitted", func() {
			aurora.WorkDir = outDir

			out, err := aurora.Run("build-iso", "-n", "def", "oci://"+image)
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(listDir(outDir)).To(ConsistOf("def.iso", "def.iso.sha256"))
		})
	})
})
