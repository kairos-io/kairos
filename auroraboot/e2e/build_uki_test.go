package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("build-uki", Label("build-uki", "e2e"), func() {
	var resultDir string
	var keysDir string
	var resultFile string
	var image string
	var err error
	var auroraboot *Auroraboot

	BeforeEach(func() {
		kairosVersion := "v3.3.3"
		resultDir, err = os.MkdirTemp("", "auroraboot-build-uki-test-")
		Expect(err).ToNot(HaveOccurred())
		image = fmt.Sprintf("quay.io/kairos/fedora:40-core-amd64-generic-%s-uki", kairosVersion)
		resultFile = filepath.Join(resultDir, fmt.Sprintf("kairos-fedora-40-core-amd64-generic-%s-uki.iso", kairosVersion))

		currentDir, err := os.Getwd()
		Expect(err).ToNot(HaveOccurred())
		keysDir = filepath.Join(currentDir, "assets", "keys")
		Expect(os.MkdirAll(keysDir, 0755)).ToNot(HaveOccurred())
		auroraboot = NewAuroraboot(resultDir, keysDir)
	})

	AfterEach(func() {
		os.RemoveAll(resultDir)
	})

	Describe("single-efi-cmdline", func() {
		BeforeEach(func() {
			By("pulling the container image")
			_, err := PullImage(image)
			Expect(err).ToNot(HaveOccurred())
			By("building the iso with single-efi-cmdline flags set")
			buildISO(auroraboot, image, keysDir, resultDir, resultFile,
				"--single-efi-cmdline", "My Entry: someoption=somevalue",
				"--single-efi-cmdline", "My Other Entry: someoption2=somevalue2")
		})

		It("creates additional .efi and .conf files", func() {
			content := listEfiFiles(auroraboot, resultFile)
			Expect(content).To(MatchRegexp("my_entry.efi"))
			Expect(content).To(MatchRegexp("my_other_entry.efi"))

			content = listConfFiles(auroraboot, resultFile)
			Expect(content).To(MatchRegexp("my_entry.conf"))
			Expect(content).To(MatchRegexp("my_other_entry.conf"))
		})
	})

	Describe("secure-boot-enroll setting in loader.conf", func() {
		When("secure-boot-enroll is not set", func() {
			BeforeEach(func() {
				By("pulling the container image")
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())
				By("building the iso with secure-boot-enroll not set")
				buildISO(auroraboot, image, keysDir, resultDir, resultFile)
			})

			It("sets the secure-boot-enroll correctly", func() {
				By("checking if the default value for secure-boot-enroll is set")
				content := readLoaderConf(auroraboot, resultFile)
				Expect(content).To(MatchRegexp("secure-boot-enroll if-safe"))
			})
		})

		When("secure-boot-enroll is set", func() {
			BeforeEach(func() {
				By("pulling the container image")
				_, err := PullImage(image)
				Expect(err).ToNot(HaveOccurred())
				By("building the iso with secure-boot-enroll set to manual")
				buildISO(auroraboot, image, keysDir, resultDir, resultFile, "--secure-boot-enroll", "manual")
			})

			It("sets the secure-boot-enroll correctly", func() {
				By("checking if the user value for secure-boot-enroll is set")
				content := readLoaderConf(auroraboot, resultFile)
				Expect(content).To(MatchRegexp("secure-boot-enroll manual"))
			})
		})
	})
})

func buildISO(auroraboot *Auroraboot, image, keysDir, resultDir, resultFile string, additionalArgs ...string) string {
	By(fmt.Sprintf("building the iso from %s", image))
	args := []string{
		"build-uki",
		"--output-dir", resultDir,
		"--public-keys", keysDir,
		"--tpm-pcr-private-key", filepath.Join(keysDir, "tpm2-pcr-private.pem"),
		"--sb-key", filepath.Join(keysDir, "db.key"),
		"--sb-cert", filepath.Join(keysDir, "db.pem"),
		"--output-type", "iso",
	}
	args = append(args, additionalArgs...)
	args = append(args, image)
	out, err := auroraboot.Run(args...)
	Expect(err).ToNot(HaveOccurred(), out)

	By("building the iso")
	_, err = os.Stat(resultFile)
	Expect(err).ToNot(HaveOccurred(), out)

	return out
}

func readLoaderConf(auroraboot *Auroraboot, isoFile string) string {
	return readEFIPartition(auroraboot, isoFile, `mtype -i "$ESP" ::/loader/loader.conf`)
}

func listEfiFiles(auroraboot *Auroraboot, isoFile string) string {
	return readEFIPartition(auroraboot, isoFile, `mdir -b -i "$ESP" ::/EFI/kairos`)
}

func listConfFiles(auroraboot *Auroraboot, isoFile string) string {
	return readEFIPartition(auroraboot, isoFile, `mdir -b -i "$ESP" ::/loader/entries`)
}

// readEFIPartition runs an mtools command against the EFI system partition
// (efiboot.img) of a build-uki ISO and returns its output.
//
// The ESP is pulled out of the ISO with xorriso and read with mtools rather
// than loop-mounted. Loop mounting cannot survive `ginkgo -p`, which runs
// these specs side by side, each in its own container:
//
//   - `losetup --find` is not atomic. It asks the kernel for a free index with
//     LOOP_CTL_GET_FREE, which reports an index without claiming it, so two
//     concurrent callers are handed the same one and only one of them wins.
//   - The container's /dev is a snapshot taken when the container starts. Any
//     loop device the kernel has to create to satisfy the demand of the other
//     specs has no node in here, so it is unusable however long we retry.
//
// mtools is also how build-uki writes this image in the first place
// (mformat/mmd/mcopy), so reading it back this way is symmetric, and it needs
// no mount, no loop device and no privileges.
func readEFIPartition(auroraboot *Auroraboot, isoFile, mtoolsCommand string) string {
	By("reading the EFI partition: " + mtoolsCommand)
	out, err := auroraboot.ContainerRun("/bin/bash", "-c",
		fmt.Sprintf(`#!/bin/bash
set -euo pipefail

ISO="%[1]s"

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

# Keep xorriso's chatter out of the output unless it actually fails.
if ! extract_out=$(xorriso -osirrox on -indev "$ISO" -extract / "$WORKDIR/iso" 2>&1); then
	echo "extracting $ISO with xorriso failed:" >&2
	echo "$extract_out" >&2
	exit 1
fi

# Rock Ridge is not guaranteed, so the recorded name may be EFIBOOT.IMG;1.
shopt -s nullglob nocaseglob
candidates=("$WORKDIR"/iso/efiboot.img*)
shopt -u nocaseglob
ESP="${candidates[0]:-}"
if [ -z "$ESP" ]; then
	echo "no efiboot.img in the root of $ISO, which contains:" >&2
	ls -la "$WORKDIR/iso" >&2
	exit 1
fi

# mformat sizes the image to its contents, so skip mtools' geometry check.
export MTOOLS_SKIP_CHECK=1
%[2]s
`, isoFile, mtoolsCommand))
	Expect(err).ToNot(HaveOccurred(), out)

	return out
}
