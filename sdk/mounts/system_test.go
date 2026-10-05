package mounts_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/mounts"
	"github.com/kairos-io/kairos/v4/sdk/state"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// testLabel is a filesystem label no host carries, so resolving it through
// the block device scan always fails and the only way a mount can happen is
// for the code to have believed a command's output instead.
const testLabel = "KAIROS_SDK_MOUNTS_TEST"

var _ = Describe("mounting a partition by its filesystem label", func() {
	var binDir, calls string

	// stub puts an executable named name ahead of the real one on PATH. It
	// appends its own argv to the calls file, so a spec can say what ran.
	stub := func(name, body string) {
		script := "#!/bin/sh\necho \"" + name + " $*\" >> " + calls + "\n" + body + "\n"
		Expect(os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755)).To(Succeed())
	}

	ran := func() []string {
		out, err := os.ReadFile(calls)
		if os.IsNotExist(err) {
			return nil
		}
		Expect(err).ToNot(HaveOccurred())
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}

	BeforeEach(func() {
		binDir = GinkgoT().TempDir()
		calls = filepath.Join(binDir, "calls")
		stub("mount", "exit 0")
		stub("umount", "exit 0")
		GinkgoT().Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	})

	// The regression. blkid resolves a label through the by-label symlinks,
	// which on an encrypted partition point at the LUKS container as often as
	// at its unlocked mapper (kairos-io/kairos#4685). Mounting the container
	// fails with "unknown filesystem type crypto_LUKS", and the label lookup
	// has to come from the block device scan instead.
	It("does not mount whatever blkid names for the label", func() {
		stub("blkid", "echo /dev/kairos-sdk-mounts-test-wrong")

		err := mounts.Mount(state.PartitionState{FilesystemLabel: testLabel}, filepath.Join(binDir, "mnt"))

		Expect(err).To(HaveOccurred(), "a label no host carries must not resolve")
		Expect(err.Error()).To(ContainSubstring(testLabel))
		Expect(ran()).ToNot(ContainElement(ContainSubstring("/dev/kairos-sdk-mounts-test-wrong")))
	})

	// A command's diagnostics and its answer come back in one string, so a
	// lookup that only checks for an empty result treats an error message as
	// a device path and hands the sentence to mount.
	It("does not mount a diagnostic line a command printed", func() {
		stub("blkid", "echo \"blkid: cannot open /dev/sr0: No medium found\" >&2\nexit 2")

		err := mounts.Mount(state.PartitionState{FilesystemLabel: testLabel}, filepath.Join(binDir, "mnt"))

		Expect(err).To(HaveOccurred())
		Expect(ran()).ToNot(ContainElement(ContainSubstring("No medium found")))
	})

	It("leaves the mount point alone when the label does not resolve", func() {
		stub("blkid", "echo /dev/kairos-sdk-mounts-test-wrong")
		mountpoint := filepath.Join(binDir, "mnt")

		Expect(mounts.Mount(state.PartitionState{FilesystemLabel: testLabel}, mountpoint)).ToNot(Succeed())
		Expect(mountpoint).ToNot(BeADirectory())
	})

	Describe("PrepareWrite", func() {
		It("only remounts a read only partition that is already where it is wanted", func() {
			stub("blkid", "echo /dev/kairos-sdk-mounts-test-wrong")
			part := state.PartitionState{
				FilesystemLabel: testLabel,
				Mounted:         true,
				IsReadOnly:      true,
				MountPoint:      "/oem",
			}

			Expect(mounts.PrepareWrite(part, "/oem")).To(Succeed())
			Expect(ran()).To(Equal([]string{"mount -o rw,remount /oem"}))
		})

		It("reports the failure when the read only partition cannot be remounted", func() {
			stub("mount", "exit 32")
			part := state.PartitionState{
				FilesystemLabel: testLabel,
				Mounted:         true,
				IsReadOnly:      true,
				MountPoint:      "/oem",
			}

			Expect(mounts.PrepareWrite(part, "/oem")).ToNot(Succeed())
		})
	})

	Describe("Umount", func() {
		It("refuses a partition that is not mounted, without running umount", func() {
			Expect(mounts.Umount(state.PartitionState{})).ToNot(Succeed())
			Expect(ran()).To(BeEmpty())
		})

		It("unmounts where the partition is mounted", func() {
			Expect(mounts.Umount(state.PartitionState{Mounted: true, MountPoint: "/oem"})).To(Succeed())
			Expect(ran()).To(Equal([]string{"umount /oem"}))
		})
	})
})
