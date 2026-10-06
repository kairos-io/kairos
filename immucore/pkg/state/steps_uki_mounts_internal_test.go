package state

import (
	"syscall"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// flagsFor returns the flags of the first entry that mounts a filesystem on
// mountpoint. The table also carries MS_SHARED propagation entries for the
// same paths, which carry no filesystem and no hardening flags, so the lookup
// skips any entry whose fs is empty.
func flagsFor(mountpoint string) (uintptr, bool) {
	for _, m := range ukiBaseMounts() {
		if m.where == mountpoint && m.fs != "" {
			return m.flags, true
		}
	}
	return 0, false
}

var _ = Describe("UKI base mount table", func() {
	// UkiPivotToSysroot moves this table into the new root with MS_MOVE, which
	// keeps the flags, so an entry mounted without MS_NOSUID stays that way for
	// the whole life of the booted system. /dev/shm is a world-writable tmpfs,
	// so it is the one entry where that matters: with neither flag set, a
	// setuid binary written there runs with its owner's privileges and a device
	// node created there is a live device.
	DescribeTable("hardens every world-writable filesystem it mounts",
		func(mountpoint string) {
			flags, found := flagsFor(mountpoint)
			Expect(found).To(BeTrue(), "%s is not mounted by the UKI base mount table", mountpoint)
			Expect(flags&syscall.MS_NOSUID).To(Equal(uintptr(syscall.MS_NOSUID)),
				"%s is mounted without MS_NOSUID", mountpoint)
			Expect(flags&syscall.MS_NODEV).To(Equal(uintptr(syscall.MS_NODEV)),
				"%s is mounted without MS_NODEV", mountpoint)
			// CIS 1.1.5 (/tmp) and 1.1.17 (/dev/shm).
			Expect(flags&syscall.MS_NOEXEC).To(Equal(uintptr(syscall.MS_NOEXEC)),
				"%s is mounted without MS_NOEXEC", mountpoint)
		},
		Entry("/dev/shm", "/dev/shm"),
		Entry("/tmp", "/tmp"),
	)

	It("keeps the propagation entries separate from the filesystem ones", func() {
		// Two entries share the /tmp mountpoint: the tmpfs and the MS_SHARED
		// remount. If flagsFor picked the second one the assertion above would
		// pass vacuously on an unhardened tmpfs, so pin which one it reads.
		var propagationOnly int
		for _, m := range ukiBaseMounts() {
			if m.fs == "" {
				Expect(m.flags).To(Equal(uintptr(syscall.MS_SHARED)))
				propagationOnly++
			}
		}
		Expect(propagationOnly).To(Equal(3), "/sys, /dev and /tmp are remounted shared")

		flags, found := flagsFor("/tmp")
		Expect(found).To(BeTrue())
		Expect(flags&syscall.MS_SHARED).To(BeZero(), "flagsFor read the propagation entry")
	})
})

var _ = Describe("GRUB /tmp and /dev/shm mounts", func() {
	It("mounts /tmp nodev, nosuid and noexec (CIS 1.1.3-1.1.5)", func() {
		Expect(cnst.TmpMountOptions).To(ContainElements("nodev", "nosuid", "noexec"))
	})

	It("remounts /dev/shm nodev, nosuid and noexec (CIS 1.1.17)", func() {
		Expect(cnst.DevShmRemountFlags & syscall.MS_REMOUNT).ToNot(BeZero())
		Expect(cnst.DevShmRemountFlags & syscall.MS_NODEV).ToNot(BeZero())
		Expect(cnst.DevShmRemountFlags & syscall.MS_NOSUID).ToNot(BeZero())
		Expect(cnst.DevShmRemountFlags & syscall.MS_NOEXEC).ToNot(BeZero())
	})
})
