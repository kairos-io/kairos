package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// writeScript drops the bundled script into a temp dir and returns its path.
func writeScript() string {
	dir := GinkgoT().TempDir()
	path := filepath.Join(dir, "kairos-jetson-qspi-update")
	Expect(os.WriteFile(path, []byte(bundled.JetsonQSPIScript), 0o755)).To(Succeed())
	return path
}

// thorCompatible is the devicetree "compatible" property of a Jetson Thor board:
// a list of NUL-separated, NUL-terminated strings, most specific first.
func thorCompatible() []byte {
	return []byte("nvidia,p3971-0089+p3834-0008\x00nvidia,tegra264\x00")
}

// orinCompatible is the same property on a non-Thor Jetson, which must be skipped.
func orinCompatible() []byte {
	return []byte("nvidia,p3737-0000+p3701-0000\x00nvidia,tegra234\x00")
}

var _ = Describe("Jetson QSPI script", func() {
	Describe("version encoding", func() {
		DescribeTable("encodes L4T versions as ESRT integers",
			func(version string, expected string) {
				out, err := exec.Command(writeScript(), "__encode_version", version).Output()
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.TrimSpace(string(out))).To(Equal(expected))
			},
			Entry("floor version", "38.0.0", "2490368"),
			Entry("previous Thor pin", "38.4.0", "2491392"),
			Entry("current Thor pin", "39.2.0", "2556416"),
			Entry("two-component version", "39.2", "2556416"),
		)

		DescribeTable("rejects malformed versions instead of encoding them wrongly",
			func(version string) {
				cmd := exec.Command(writeScript(), "__encode_version", version)
				out, err := cmd.CombinedOutput()
				Expect(err).To(HaveOccurred(),
					"expected %q to be rejected, got output %q", version, string(out))
			},
			Entry("single component", "39"),
			Entry("four components", "39.2.0.1"),
			Entry("not a version at all", "not-a-version"),
			Entry("empty", ""),
			Entry("trailing dot", "39.2."),
			Entry("non-numeric component", "39.x.0"),
		)

		DescribeTable("decodes ESRT integers back to L4T versions",
			func(encoded string, expected string) {
				out, err := exec.Command(writeScript(), "__decode_version", encoded).Output()
				Expect(err).ToNot(HaveOccurred())
				Expect(strings.TrimSpace(string(out))).To(Equal(expected))
			},
			Entry("floor version", "2490368", "38.0.0"),
			Entry("previous Thor pin", "2491392", "38.4.0"),
			Entry("current Thor pin", "2556416", "39.2.0"),
		)
	})

	Describe("board detection", func() {
		// dtEnv builds a fixture environment with a devicetree compatible file
		// holding the given bytes, plus a valid ESRT reading.
		dtEnv := func(compatible []byte) []string {
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			dt := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dt, compatible, 0o644)).To(Succeed())
			return append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+dt,
				"KAIROS_QSPI_DRY_RUN=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
		}

		run := func(envv []string) (string, error) {
			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			return string(out), err
		}

		It("proceeds on a Thor board (nvidia,tegra264)", func() {
			out, err := run(dtEnv(thorCompatible()))
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("staging capsule"))
		})

		It("skips cleanly on non-Thor hardware", func() {
			out, err := run(dtEnv(orinCompatible()))
			Expect(err).ToNot(HaveOccurred(), out)
			Expect(out).To(ContainSubstring("not an nvidia,tegra264"))
			Expect(out).ToNot(ContainSubstring("staging capsule"))
		})

		It("aborts rather than skipping when the compatible file is missing", func() {
			// A silent skip here is exactly the bug that made this feature inert:
			// inconclusive detection must never look like "not a Thor".
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+filepath.Join(dir, "absent"),
				"KAIROS_QSPI_DRY_RUN=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("cannot read"))
			Expect(string(out)).To(ContainSubstring("whether this board is a Jetson Thor"))
		})

		It("aborts rather than skipping when the compatible file is empty", func() {
			out, err := run(dtEnv([]byte{}))
			Expect(err).To(HaveOccurred())
			Expect(out).To(ContainSubstring("is empty"))
		})

		It("does not depend on /etc/nv_boot_control.conf", func() {
			// No L4T package ships that file, so gating on it made the whole
			// feature a silent no-op on every real Kairos image.
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("nv_boot_control"))
			data, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/13_nvidia_qspi.yaml")
			Expect(err).ToNot(HaveOccurred())
			Expect(string(data)).ToNot(ContainSubstring("nv_boot_control"))
		})
	})

	Describe("decision logic", func() {
		// env builds a fixture environment: an ESRT file holding the board's current
		// firmware version, and a devicetree compatible file declaring a Thor SoC.
		env := func(currentQSPI string) (string, []string) {
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte(currentQSPI+"\n"), 0o644)).To(Succeed())
			dt := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dt, thorCompatible(), 0o644)).To(Succeed())
			return dir, append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+dt,
				"KAIROS_QSPI_DRY_RUN=1",
			)
		}

		run := func(envv []string, imageVersion string) (string, error) {
			cmd := exec.Command(writeScript())
			cmd.Env = append(envv, "KAIROS_QSPI_IMAGE_VERSION="+imageVersion)
			out, err := cmd.CombinedOutput()
			return string(out), err
		}

		It("stages a capsule when the image is newer than the board", func() {
			_, envv := env("2490368") // board 38.0.0
			out, err := run(envv, "39.2.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("staging capsule"))
		})

		It("does nothing when versions match", func() {
			_, envv := env("2556416")
			out, err := run(envv, "39.2.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("already matches"))
			Expect(out).ToNot(ContainSubstring("staging capsule"))
		})

		It("aborts when the board firmware is newer than the image", func() {
			_, envv := env("2556416")       // board 39.2.0
			out, err := run(envv, "38.4.0") // image 38.4.0
			Expect(err).To(HaveOccurred())
			Expect(out).To(ContainSubstring("newer than this image"))
			Expect(out).To(ContainSubstring("2556416"))
			Expect(out).To(ContainSubstring("2491392"))
		})

		It("reports human-readable versions, not just raw integers", func() {
			_, envv := env("2556416")
			out, err := run(envv, "38.4.0")
			Expect(err).To(HaveOccurred())
			Expect(out).To(ContainSubstring("39.2.0"))
			Expect(out).To(ContainSubstring("38.4.0"))
			// The advice line is the one the bug reporter reads.
			Expect(out).To(ContainSubstring("built for L4T 39.2.0"))
		})

		It("aborts when the board is below the 38.0.0 floor", func() {
			_, envv := env("2424832") // 37.0.0
			out, err := run(envv, "39.2.0")
			Expect(err).To(HaveOccurred())
			Expect(out).To(ContainSubstring("USB host flash"))
			Expect(out).To(ContainSubstring("37.0.0"))
			Expect(out).To(ContainSubstring("floor 38.0.0"))
		})

		It("logs decoded versions on the informational line", func() {
			_, envv := env("2490368")
			out, err := run(envv, "39.2.0")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(ContainSubstring("board firmware: 38.0.0"))
			Expect(out).To(ContainSubstring("image L4T: 39.2.0"))
		})

		It("aborts when ESRT is unreadable", func() {
			dir := GinkgoT().TempDir()
			dt := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dt, thorCompatible(), 0o644)).To(Succeed())
			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+filepath.Join(dir, "absent"),
				"DT_COMPATIBLE_FILE="+dt,
				"KAIROS_QSPI_DRY_RUN=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("cannot read current firmware version"))
		})

		It("aborts loudly when the ESRT path is a directory", func() {
			// Regression guard: a bare var="$(cmd)" would die here silently,
			// with no [kairos-qspi] ERROR line at all.
			dir := GinkgoT().TempDir()
			dt := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dt, thorCompatible(), 0o644)).To(Succeed())
			esrtDir := filepath.Join(dir, "esrt-as-a-dir")
			Expect(os.MkdirAll(esrtDir, 0o755)).To(Succeed())
			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrtDir,
				"DT_COMPATIBLE_FILE="+dt,
				"KAIROS_QSPI_DRY_RUN=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("[kairos-qspi] ERROR"))
			Expect(string(out)).To(ContainSubstring("cannot read current firmware version"))
		})

		// failingDpkgQueryDir writes a "dpkg-query" shim that always fails and
		// returns its directory, so prepending it to PATH shadows any real
		// dpkg-query without disturbing the other coreutils (awk, cut, tr, head)
		// the script depends on for the steps before the bootloader-version check.
		failingDpkgQueryDir := func() string {
			dir := GinkgoT().TempDir()
			shim := filepath.Join(dir, "dpkg-query")
			Expect(os.WriteFile(shim, []byte("#!/bin/sh\nexit 1\n"), 0o755)).To(Succeed())
			return dir
		}

		It("aborts with a clear message when the bootloader version cannot be determined on the real (non-test) path", func() {
			// Leave KAIROS_QSPI_IMAGE_VERSION unset so the script takes the
			// production dpkg-query branch, not the test seam. Shadow
			// dpkg-query with a failing shim so the command substitution fails
			// the way it would on an image without nvidia-l4t-bootloader installed.
			_, envv := env("2490368")
			for i, e := range envv {
				if strings.HasPrefix(e, "PATH=") {
					envv[i] = "PATH=" + failingDpkgQueryDir() + string(os.PathListSeparator) + strings.TrimPrefix(e, "PATH=")
				}
			}

			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("cannot determine the L4T bootloader version in this image"))
		})

		It("aborts with a clear message when the image L4T version is malformed", func() {
			_, envv := env("2490368")
			envv = append(envv, "KAIROS_QSPI_IMAGE_VERSION=not-a-version")
			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("cannot parse"))
			Expect(string(out)).To(ContainSubstring("not-a-version"))
		})
	})

	Describe("capsule staging", func() {
		// stagingFixture lays out a complete staging environment and returns the
		// temp dir plus the environment to run the script with.
		stagingFixture := func(capsuleNames ...string) (string, []string) {
			dir := GinkgoT().TempDir()

			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			dt := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dt, thorCompatible(), 0o644)).To(Succeed())

			ota := filepath.Join(dir, "ota", "t26x")
			Expect(os.MkdirAll(ota, 0o755)).To(Succeed())
			for _, name := range capsuleNames {
				Expect(os.WriteFile(filepath.Join(ota, name),
					[]byte("CAPSULE-PAYLOAD"), 0o644)).To(Succeed())
			}

			esp := filepath.Join(dir, "esp")
			Expect(os.MkdirAll(esp, 0o755)).To(Succeed())
			efivars := filepath.Join(dir, "efivars")
			Expect(os.MkdirAll(efivars, 0o755)).To(Succeed())

			return dir, append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+dt,
				"OTA_PACKAGE_DIR="+filepath.Join(dir, "ota"),
				"EFIVARS_DIR="+efivars,
				"ESP_MOUNT_DIR="+esp,
				"KAIROS_QSPI_SKIP_MOUNT=1",
				"KAIROS_QSPI_SKIP_EFIVARS_CHECK=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
		}

		It("copies the capsule to the ESP and sets OsIndications", func() {
			dir, envv := stagingFixture("TEGRA_BL_3834_agx.Cap")

			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))

			staged, err := os.ReadFile(filepath.Join(dir, "esp", "EFI", "UpdateCapsule", "TEGRA_BL_3834_agx.Cap"))
			Expect(err).ToNot(HaveOccurred())
			Expect(string(staged)).To(Equal("CAPSULE-PAYLOAD"))

			v, err := os.ReadFile(filepath.Join(dir, "efivars",
				"OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c"))
			Expect(err).ToNot(HaveOccurred())
			Expect(v).To(Equal([]byte{0x07, 0, 0, 0, 0x04, 0, 0, 0, 0, 0, 0, 0}))
		})

		It("finds the capsule regardless of its name", func() {
			// NVIDIA renames the capsule between L4T releases; hardcoding the
			// name turns a routine version bump into a hard install failure.
			dir, envv := stagingFixture("TEGRA_BL_9999_somethingelse.Cap")

			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).ToNot(HaveOccurred(), string(out))
			Expect(filepath.Join(dir, "esp", "EFI", "UpdateCapsule", "TEGRA_BL_9999_somethingelse.Cap")).To(BeAnExistingFile())
		})

		It("aborts when no capsule is present", func() {
			_, envv := stagingFixture()
			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("no capsule payload"))
			Expect(string(out)).To(ContainSubstring("t26x"))
		})

		It("aborts when more than one capsule is present", func() {
			_, envv := stagingFixture("TEGRA_BL_A.Cap", "TEGRA_BL_B.Cap")
			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("expected exactly one capsule payload"))
			Expect(string(out)).To(ContainSubstring("TEGRA_BL_A.Cap"))
			Expect(string(out)).To(ContainSubstring("TEGRA_BL_B.Cap"))
		})

		It("never writes a bootloader into EFI/BOOT", func() {
			// Regression guard: NVIDIA's postinst copies L4TLauncher over
			// EFI/BOOT/BOOTAA64.efi, which on Kairos is our own bootloader.
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("BOOTAA64"))
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("extlinux"))
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("dpkg-reconfigure"))
		})

		DescribeTable("aborts when free space cannot be trusted",
			func(forcedFreeKB string, expected string) {
				_, envv := stagingFixture("TEGRA_BL_3834_agx.Cap")
				envv = append(envv, "KAIROS_QSPI_FORCE_FREE_KB="+forcedFreeKB)
				cmd := exec.Command(writeScript())
				cmd.Env = envv
				out, err := cmd.CombinedOutput()
				Expect(err).To(HaveOccurred(), string(out))
				Expect(string(out)).To(ContainSubstring(expected))
			},
			Entry("full ESP", "0", "not enough free space"),
			// A non-numeric or empty value must not slip past the guard: [ x -le y ]
			// returns 2, which set -e does not catch inside an if condition.
			Entry("non-numeric free space", "not-a-number", "cannot determine free space"),
			Entry("df output with units", "12K", "cannot determine free space"),
		)

		It("aborts when free space comes back empty from a successful pipeline", func() {
			// pipefail catches a failing df, but not a df that succeeds and
			// prints nothing.
			_, envv := stagingFixture("TEGRA_BL_3834_agx.Cap")
			shimDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(shimDir, "df"),
				[]byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())
			for i, e := range envv {
				if strings.HasPrefix(e, "PATH=") {
					envv[i] = "PATH=" + shimDir + string(os.PathListSeparator) + strings.TrimPrefix(e, "PATH=")
				}
			}
			cmd := exec.Command(writeScript())
			cmd.Env = envv
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred(), string(out))
			Expect(string(out)).To(ContainSubstring("cannot determine free space"))
		})
	})

	Describe("efivars handling", func() {
		// This is the most important test on this script. kairos-agent bind-mounts
		// /sys into the install chroot NON-recursively, so efivarfs is not carried
		// in and /sys/firmware/efi/efivars is a plain, empty sysfs directory there.
		// Writing a regular file into it would look like a complete success while
		// staging nothing, and the user would boot a Thor board to a black screen.
		It("aborts instead of reporting success when the target is not efivarfs", func() {
			if os.Geteuid() == 0 {
				// As root, "mount -t efivarfs" below would not fail with EPERM the
				// way it does as an unprivileged test user - it would really mount
				// efivarfs onto this Ginkgo temp dir. Skip rather than risk that;
				// the shim-based test alongside this one exercises the same
				// aborts-when-not-efivarfs guarantee without depending on
				// privilege level or ever invoking the real mount(8) at all.
				Skip("requires non-root privileges to safely exercise the real mount(8) EPERM failure")
			}
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			dtFile := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dtFile, thorCompatible(), 0o644)).To(Succeed())
			ota := filepath.Join(dir, "ota", "t26x")
			Expect(os.MkdirAll(ota, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(ota, "TEGRA_BL_3834_agx.Cap"),
				[]byte("CAPSULE-PAYLOAD"), 0o644)).To(Succeed())
			esp := filepath.Join(dir, "esp")
			Expect(os.MkdirAll(esp, 0o755)).To(Succeed())
			// A plain directory standing in for the chroot's empty sysfs path.
			efivars := filepath.Join(dir, "efivars")
			Expect(os.MkdirAll(efivars, 0o755)).To(Succeed())

			cmd := exec.Command(writeScript())
			// Note: KAIROS_QSPI_SKIP_EFIVARS_CHECK is deliberately NOT set.
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+dtFile,
				"OTA_PACKAGE_DIR="+filepath.Join(dir, "ota"),
				"EFIVARS_DIR="+efivars,
				"ESP_MOUNT_DIR="+esp,
				"KAIROS_QSPI_SKIP_MOUNT=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(),
				"the script reported success while writing the capsule flag to a plain directory: %s", string(out))
			Expect(string(out)).ToNot(ContainSubstring("firmware will be updated on the next boot"))
			// It must not have left a fake variable behind either.
			Expect(filepath.Join(efivars, "OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c")).
				ToNot(BeAnExistingFile())
		})

		It("aborts instead of reporting success when mount succeeds but the target still isn't efivarfs", func() {
			// This is the branch that actually matters in production: root,
			// inside the chroot, "mount -t efivarfs" succeeds - but the chroot's
			// /sys/firmware/efi/efivars is only ever a plain sysfs directory
			// (see the big comment above the efivarfs section in the script), so
			// the re-verification after mount must catch it and abort. The test
			// above only proves "mount refused" (EPERM as a non-root test user);
			// it never reaches this branch. A fake "mount" shim that exits 0
			// without mounting anything drives the script into it deterministically,
			// without ever invoking the real mount(8) - so this test is safe to run
			// as any user, including root on a UEFI host.
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			dtFile := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dtFile, thorCompatible(), 0o644)).To(Succeed())
			ota := filepath.Join(dir, "ota", "t26x")
			Expect(os.MkdirAll(ota, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(ota, "TEGRA_BL_3834_agx.Cap"),
				[]byte("CAPSULE-PAYLOAD"), 0o644)).To(Succeed())
			esp := filepath.Join(dir, "esp")
			Expect(os.MkdirAll(esp, 0o755)).To(Succeed())
			// A plain directory standing in for the chroot's empty sysfs path,
			// same as the EPERM test above - "mount" succeeding must not be
			// enough on its own.
			efivars := filepath.Join(dir, "efivars")
			Expect(os.MkdirAll(efivars, 0o755)).To(Succeed())

			shimDir := GinkgoT().TempDir()
			Expect(os.WriteFile(filepath.Join(shimDir, "mount"),
				[]byte("#!/bin/sh\nexit 0\n"), 0o755)).To(Succeed())

			cmd := exec.Command(writeScript())
			// Note: KAIROS_QSPI_SKIP_EFIVARS_CHECK is deliberately NOT set.
			env := append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+dtFile,
				"OTA_PACKAGE_DIR="+filepath.Join(dir, "ota"),
				"EFIVARS_DIR="+efivars,
				"ESP_MOUNT_DIR="+esp,
				"KAIROS_QSPI_SKIP_MOUNT=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			for i, e := range env {
				if strings.HasPrefix(e, "PATH=") {
					env[i] = "PATH=" + shimDir + string(os.PathListSeparator) + strings.TrimPrefix(e, "PATH=")
				}
			}
			cmd.Env = env
			out, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred(),
				"the script reported success after a mount that didn't actually produce efivarfs: %s", string(out))
			Expect(string(out)).ToNot(ContainSubstring("firmware will be updated on the next boot"))
			Expect(filepath.Join(efivars, "OsIndications-8be4df61-93ca-11d2-aa0d-00e098032b8c")).
				ToNot(BeAnExistingFile())
		})

		It("aborts when the efivars directory does not exist, and never creates it", func() {
			dir := GinkgoT().TempDir()
			esrt := filepath.Join(dir, "fw_version")
			Expect(os.WriteFile(esrt, []byte("2490368\n"), 0o644)).To(Succeed())
			dtFile := filepath.Join(dir, "compatible")
			Expect(os.WriteFile(dtFile, thorCompatible(), 0o644)).To(Succeed())
			ota := filepath.Join(dir, "ota", "t26x")
			Expect(os.MkdirAll(ota, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(ota, "TEGRA_BL_3834_agx.Cap"),
				[]byte("CAPSULE-PAYLOAD"), 0o644)).To(Succeed())
			esp := filepath.Join(dir, "esp")
			Expect(os.MkdirAll(esp, 0o755)).To(Succeed())
			efivars := filepath.Join(dir, "no-such-efivars")

			cmd := exec.Command(writeScript())
			cmd.Env = append(os.Environ(),
				"ESRT_FW_VERSION_FILE="+esrt,
				"DT_COMPATIBLE_FILE="+dtFile,
				"OTA_PACKAGE_DIR="+filepath.Join(dir, "ota"),
				"EFIVARS_DIR="+efivars,
				"ESP_MOUNT_DIR="+esp,
				"KAIROS_QSPI_SKIP_MOUNT=1",
				"KAIROS_QSPI_IMAGE_VERSION=39.2.0",
			)
			out, err := cmd.CombinedOutput()
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("does not exist"))
			Expect(efivars).ToNot(BeADirectory())
		})

		It("does not create the efivars directory anywhere in the script", func() {
			// mkdir -p on the efivars path is precisely what made the failure
			// fail-open. Keep it out.
			Expect(bundled.JetsonQSPIScript).ToNot(ContainSubstring("mkdir -p \"${EFIVARS_DIR}\""))
			Expect(bundled.JetsonQSPIScript).To(ContainSubstring("mount -t efivarfs"))
		})
	})
})

var _ = Describe("Jetson QSPI cloud-config", func() {
	It("is embedded and runs on after-install-chroot", func() {
		data, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/13_nvidia_qspi.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("after-install-chroot"))
		Expect(string(data)).To(ContainSubstring(bundled.JetsonQSPIScriptPath))
	})

	It("also runs on after-upgrade-chroot so OCI upgrades restage a newer capsule", func() {
		// An OCI upgrade never fires the after-install hook, so without this the
		// newer bootloader capsule an upgraded image ships would never be staged
		// and the board would keep booting old firmware into a version mismatch.
		data, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/13_nvidia_qspi.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("after-upgrade-chroot"))
		// The upgrade stage must invoke the same script, not a soft-gated variant:
		// both stages run the identical self-gating command.
		Expect(strings.Count(string(data), bundled.JetsonQSPIScriptPath+"\n")).To(Equal(2))
	})

	It("delegates board detection entirely to the script, not the YAML guard", func() {
		// The compatible-string check used to live here too. That meant an
		// unreadable compatible file silently skipped this whole stage before
		// the script's own fail-closed logic (the fix for the bug that made
		// this feature inert) ever got a chance to run. The YAML guard must
		// do nothing more than check the script is present; the script itself
		// decides Thor-vs-not and exits 0 cleanly on non-Thor boards.
		data, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/13_nvidia_qspi.yaml")
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).ToNot(ContainSubstring("devicetree"))
		Expect(string(data)).ToNot(ContainSubstring("tegra264"))
	})
})
