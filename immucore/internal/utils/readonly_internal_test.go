package utils

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// setCmdline points GetHostProcCmdline at a fixture holding content, for one
// spec, and re-arms the memoized HardwareRO answer so specs do not inherit each
// other's. Same shape as installFakeBlkid in mounts_internal_test.go.
func setCmdline(content string) {
	path := filepath.Join(GinkgoT().TempDir(), "cmdline")
	Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())

	previous, had := os.LookupEnv("HOST_PROC_CMDLINE")
	Expect(os.Setenv("HOST_PROC_CMDLINE", path)).To(Succeed())

	previousHardwareRO := HardwareRO
	HardwareRO = sync.OnceValue(detectHardwareRO)
	previousDelay, previousAttempts := hardwareRORetryDelay, hardwareRORetryAttempts
	hardwareRORetryDelay, hardwareRORetryAttempts = 0, 2

	DeferCleanup(func() {
		HardwareRO = previousHardwareRO
		hardwareRORetryDelay, hardwareRORetryAttempts = previousDelay, previousAttempts
		if had {
			Expect(os.Setenv("HOST_PROC_CMDLINE", previous)).To(Succeed())
			return
		}
		Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
	})
}

// answerProbeWith installs a device probe for one spec.
func answerProbeWith(fn func(string) (bool, error)) {
	previous := deviceReadOnly
	deviceReadOnly = fn
	DeferCleanup(func() { deviceReadOnly = previous })
}

// answerProbe makes the device probe give a fixed answer for one spec.
func answerProbe(ro bool, err error) {
	answerProbeWith(func(string) (bool, error) { return ro, err })
}

var _ = Describe("parseHardwareRO", func() {
	It("reports not set when the stanza is absent", func() {
		setCmdline("root=LABEL=COS_ACTIVE rd.immucore.debug")
		_, set := parseHardwareRO()
		Expect(set).To(BeFalse())
	})

	It("reads a bare token as on", func() {
		setCmdline("root=LABEL=COS_ACTIVE rd.immucore.hardware_ro")
		forced, set := parseHardwareRO()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})

	It("reads =0 as off", func() {
		setCmdline("rd.immucore.hardware_ro=0")
		forced, set := parseHardwareRO()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeFalse())
	})

	It("reads =1 as on", func() {
		setCmdline("rd.immucore.hardware_ro=1")
		forced, set := parseHardwareRO()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})

	// A prefix match would have read this as a request to turn the layout on,
	// which silently makes every persistent write ephemeral. Exact tokens only.
	It("does not match a stanza that merely starts with the key", func() {
		setCmdline("rd.immucore.hardware_rox")
		_, set := parseHardwareRO()
		Expect(set).To(BeFalse())
	})

	It("does not match =10 as off", func() {
		// 10 is not one of the off spellings, so it is on. It has to get there
		// by parsing the value, not by a substring match on "=1", which would
		// also read =10 as on but for the wrong reason.
		setCmdline("rd.immucore.hardware_ro=10")
		forced, set := parseHardwareRO()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})
})

var _ = Describe("hardwareROCandidates", func() {
	// Detection runs on every boot, live media and rd.immucore.disable included.
	// GetState() panics after ten seconds of retries when there is no state label
	// to find, so the candidate list must not be built from it.
	It("is built from plain by-label paths only", func() {
		candidates := hardwareROCandidates()
		Expect(candidates).ToNot(BeEmpty())
		for _, c := range candidates {
			Expect(c).To(HavePrefix("/dev/disk/by-label/"))
			Expect(c).ToNot(Equal("/dev/disk/by-label"), "an empty label would probe the directory itself")
		}
	})

	It("asks about the persistent partition before anything else", func() {
		// It is the partition whose writability the layout turns on, and custom
		// partitioning can put it on a different disk than the state partition.
		Expect(hardwareROCandidates()[0]).To(Equal("/dev/disk/by-label/COS_PERSISTENT"))
	})

	It("covers the encrypted and recovery shapes too", func() {
		Expect(hardwareROCandidates()).To(ContainElements(
			"/dev/disk/by-label/COS_PERSISTENT_LUKS",
			"/dev/disk/by-label/COS_STATE",
			"/dev/disk/by-label/COS_RECOVERY",
		))
	})
})

var _ = Describe("HardwareRO", func() {
	It("lets the cmdline override a device that says writable", func() {
		setCmdline("rd.immucore.hardware_ro")
		answerProbe(false, nil)
		Expect(HardwareRO()).To(BeTrue())
	})

	It("lets the cmdline override a device that says read-only", func() {
		setCmdline("rd.immucore.hardware_ro=0")
		answerProbe(true, nil)
		Expect(HardwareRO()).To(BeFalse())
	})

	It("uses the device when the cmdline says nothing", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbe(true, nil)
		Expect(HardwareRO()).To(BeTrue())
	})

	It("reports writable when the device says writable", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbe(false, nil)
		Expect(HardwareRO()).To(BeFalse())
	})

	It("reports writable when no device can answer", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbe(false, errors.New("no such device"))
		Expect(HardwareRO()).To(BeFalse())
	})

	It("is memoized, so the probe runs once however often it is asked", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		calls := 0
		answerProbeWith(func(string) (bool, error) {
			calls++
			return true, nil
		})

		Expect(HardwareRO()).To(BeTrue())
		Expect(HardwareRO()).To(BeTrue())
		Expect(HardwareRO()).To(BeTrue())
		Expect(calls).To(Equal(1), "Fsck asks this once per mount attempt inside a retry loop")
	})
})

var _ = Describe("HardwareRO candidate order", func() {
	It("takes the first device that answers, not the first device", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbeWith(func(device string) (bool, error) {
			if device == "/dev/disk/by-label/COS_PERSISTENT" {
				return false, errors.New("no such file or directory")
			}
			return true, nil
		})
		Expect(HardwareRO()).To(BeTrue())
	})

	It("waits for a label that appears late", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		attempt := 0
		answerProbeWith(func(string) (bool, error) {
			attempt++
			if attempt < 3 {
				return false, errors.New("not yet")
			}
			return true, nil
		})
		hardwareRORetryAttempts = 5
		Expect(HardwareRO()).To(BeTrue())
		Expect(attempt).To(BeNumerically(">=", 3))
	})

	It("does not probe at all on a boot the layout does not apply to", func() {
		setCmdline("root=live:LABEL=COS_LIVE rd.cos.disable")
		answerProbeWith(func(string) (bool, error) {
			Fail("the probe ran on live media")
			return false, nil
		})
		Expect(HardwareRO()).To(BeFalse())
	})
})

var _ = Describe("parseHardwareRO edge cases", func() {
	It("reads the other off spellings", func() {
		for _, v := range []string{"=false", "=no"} {
			setCmdline("rd.immucore.hardware_ro" + v)
			forced, set := parseHardwareRO()
			Expect(set).To(BeTrue(), v)
			Expect(forced).To(BeFalse(), v)
		}
	})

	It("reads an empty value as on, like the bare token", func() {
		setCmdline("rd.immucore.hardware_ro=")
		forced, set := parseHardwareRO()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})

	It("lets the last of two tokens win", func() {
		setCmdline("rd.immucore.hardware_ro rd.immucore.hardware_ro=0")
		forced, _ := parseHardwareRO()
		Expect(forced).To(BeFalse())
	})
})

var _ = Describe("ReadOnlyMountOptions", func() {
	// "ro" on its own is not enough: the kernel replays a dirty ext4 journal
	// even on a read-only mount unless noload is given, and the replay is a
	// write. That is the whole reason this helper exists.
	It("keeps ext4 from replaying its journal", func() {
		Expect(ReadOnlyMountOptions("ext4")).To(Equal([]string{"ro", "noload"}))
	})

	It("covers ext3, which also has a journal", func() {
		Expect(ReadOnlyMountOptions("ext3")).To(Equal([]string{"ro", "noload"}))
	})

	It("does not hand noload to ext2, which the kernel rejects", func() {
		// The active and passive images are ext2. With noload the root mount
		// failed three times with EINVAL on a real boot and only survived
		// because dracut had mounted /sysroot first; the fstab line it left
		// behind then broke systemd-remount-fs after switch_root.
		Expect(ReadOnlyMountOptions("ext2")).To(Equal([]string{"ro"}))
	})

	It("uses xfs's spelling for xfs", func() {
		Expect(ReadOnlyMountOptions("xfs")).To(Equal([]string{"ro", "norecovery"}))
	})

	It("falls back to a plain ro rather than guessing an option the kernel may reject", func() {
		Expect(ReadOnlyMountOptions("btrfs")).To(Equal([]string{"ro"}))
		Expect(ReadOnlyMountOptions("")).To(Equal([]string{"ro"}))
	})
})

var _ = Describe("Fsck on write-protected media", func() {
	It("does not run at all", func() {
		setCmdline("rd.immucore.hardware_ro")
		answerProbe(false, nil)

		// A device that cannot exist, so a fsck that did run would fail. The
		// gate is what makes this nil. It sits before the mount in
		// MountOPWithFstab's PrepareCallback, so no mount option can stand in
		// for it.
		Expect(Fsck("/dev/immucore-does-not-exist")).To(Succeed())
	})

	It("skips a read-only device even when the global answer is forced off", func() {
		setCmdline("rd.immucore.hardware_ro=0")
		answerProbe(true, nil)
		Expect(Fsck("/dev/immucore-does-not-exist")).To(Succeed())
	})
})
