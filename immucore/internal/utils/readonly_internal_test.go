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
// spec, and re-arms the memoized WriteProtected answer so specs do not inherit each
// other's. Same shape as installFakeBlkid in mounts_internal_test.go.
func setCmdline(content string) {
	path := filepath.Join(GinkgoT().TempDir(), "cmdline")
	Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())

	previous, had := os.LookupEnv("HOST_PROC_CMDLINE")
	Expect(os.Setenv("HOST_PROC_CMDLINE", path)).To(Succeed())

	previousWriteProtected := WriteProtected
	WriteProtected = sync.OnceValue(detectWriteProtected)
	previousDelay, previousAttempts := writeProtectedRetryDelay, writeProtectedRetryAttempts
	writeProtectedRetryDelay, writeProtectedRetryAttempts = 0, 2

	DeferCleanup(func() {
		WriteProtected = previousWriteProtected
		writeProtectedRetryDelay, writeProtectedRetryAttempts = previousDelay, previousAttempts
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

var _ = Describe("parseWriteProtected", func() {
	It("reports not set when the stanza is absent", func() {
		setCmdline("root=LABEL=COS_ACTIVE rd.immucore.debug")
		_, set := parseWriteProtected()
		Expect(set).To(BeFalse())
	})

	It("reads a bare token as on", func() {
		setCmdline("root=LABEL=COS_ACTIVE rd.immucore.write_protected")
		forced, set := parseWriteProtected()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})

	It("reads =0 as off", func() {
		setCmdline("rd.immucore.write_protected=0")
		forced, set := parseWriteProtected()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeFalse())
	})

	It("reads =1 as on", func() {
		setCmdline("rd.immucore.write_protected=1")
		forced, set := parseWriteProtected()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})

	// A prefix match would have read this as a request to turn the layout on,
	// which silently makes every persistent write ephemeral. Exact tokens only.
	It("does not match a stanza that merely starts with the key", func() {
		setCmdline("rd.immucore.write_protectedx")
		_, set := parseWriteProtected()
		Expect(set).To(BeFalse())
	})

	It("does not match =10 as off", func() {
		// 10 is not one of the off spellings, so it is on. It has to get there
		// by parsing the value, not by a substring match on "=1", which would
		// also read =10 as on but for the wrong reason.
		setCmdline("rd.immucore.write_protected=10")
		forced, set := parseWriteProtected()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})
})

var _ = Describe("writeProtectedCandidates", func() {
	// Detection runs on every boot, live media and rd.immucore.disable included.
	// GetState() panics after ten seconds of retries when there is no state label
	// to find, so the candidate list must not be built from it.
	It("is built from plain by-label paths only", func() {
		candidates := writeProtectedCandidates()
		Expect(candidates).ToNot(BeEmpty())
		for _, c := range candidates {
			Expect(c).To(HavePrefix("/dev/disk/by-label/"))
			Expect(c).ToNot(Equal("/dev/disk/by-label"), "an empty label would probe the directory itself")
		}
	})

	It("asks about the persistent partition before anything else", func() {
		// It is the partition whose writability the layout turns on, and custom
		// partitioning can put it on a different disk than the state partition.
		Expect(writeProtectedCandidates()[0]).To(Equal("/dev/disk/by-label/COS_PERSISTENT"))
	})

	It("covers the encrypted and recovery shapes too", func() {
		Expect(writeProtectedCandidates()).To(ContainElements(
			"/dev/disk/by-label/COS_PERSISTENT_LUKS",
			"/dev/disk/by-label/COS_STATE",
			"/dev/disk/by-label/COS_RECOVERY",
		))
	})
})

var _ = Describe("WriteProtected", func() {
	It("lets the cmdline override a device that says writable", func() {
		setCmdline("rd.immucore.write_protected")
		answerProbe(false, nil)
		Expect(WriteProtected()).To(BeTrue())
	})

	It("lets the cmdline override a device that says read-only", func() {
		setCmdline("rd.immucore.write_protected=0")
		answerProbe(true, nil)
		Expect(WriteProtected()).To(BeFalse())
	})

	It("uses the device when the cmdline says nothing", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbe(true, nil)
		Expect(WriteProtected()).To(BeTrue())
	})

	It("reports writable when the device says writable", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbe(false, nil)
		Expect(WriteProtected()).To(BeFalse())
	})

	It("reports writable when no device can answer", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbe(false, errors.New("no such device"))
		Expect(WriteProtected()).To(BeFalse())
	})

	It("is memoized, so the probe runs once however often it is asked", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		calls := 0
		answerProbeWith(func(string) (bool, error) {
			calls++
			return true, nil
		})

		Expect(WriteProtected()).To(BeTrue())
		Expect(WriteProtected()).To(BeTrue())
		Expect(WriteProtected()).To(BeTrue())
		Expect(calls).To(Equal(1), "Fsck asks this once per mount attempt inside a retry loop")
	})
})

var _ = Describe("WriteProtected candidate order", func() {
	It("takes the first device that answers, not the first device", func() {
		setCmdline("root=LABEL=COS_ACTIVE")
		answerProbeWith(func(device string) (bool, error) {
			if device == "/dev/disk/by-label/COS_PERSISTENT" {
				return false, errors.New("no such file or directory")
			}
			return true, nil
		})
		Expect(WriteProtected()).To(BeTrue())
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
		writeProtectedRetryAttempts = 5
		Expect(WriteProtected()).To(BeTrue())
		Expect(attempt).To(BeNumerically(">=", 3))
	})

	It("does not probe at all on a boot the layout does not apply to", func() {
		setCmdline("root=live:LABEL=COS_LIVE rd.cos.disable")
		answerProbeWith(func(string) (bool, error) {
			Fail("the probe ran on live media")
			return false, nil
		})
		Expect(WriteProtected()).To(BeFalse())
	})
})

var _ = Describe("parseWriteProtected auto and the store sub-key", func() {
	It("hands =auto back to the probe", func() {
		setCmdline("rd.immucore.write_protected=auto")
		forced, set := parseWriteProtected()
		Expect(set).To(BeFalse())
		Expect(forced).To(BeFalse())
	})

	It("lets a later =auto undo an earlier forced value", func() {
		setCmdline("rd.immucore.write_protected=1 rd.immucore.write_protected=auto")
		_, set := parseWriteProtected()
		Expect(set).To(BeFalse())
	})

	It("does not read the store size sub-key as the flag", func() {
		// rd.immucore.write_protected.cow= shares the prefix; a prefix match would
		// turn the layout on for anyone who only wanted to size the store.
		setCmdline("rd.immucore.write_protected.cow=tmpfs:2G")
		_, set := parseWriteProtected()
		Expect(set).To(BeFalse())
	})
})

var _ = Describe("parseWriteProtected edge cases", func() {
	It("reads the other off spellings", func() {
		for _, v := range []string{"=false", "=no"} {
			setCmdline("rd.immucore.write_protected" + v)
			forced, set := parseWriteProtected()
			Expect(set).To(BeTrue(), v)
			Expect(forced).To(BeFalse(), v)
		}
	})

	It("reads an empty value as on, like the bare token", func() {
		setCmdline("rd.immucore.write_protected=")
		forced, set := parseWriteProtected()
		Expect(set).To(BeTrue())
		Expect(forced).To(BeTrue())
	})

	It("lets the last of two tokens win", func() {
		setCmdline("rd.immucore.write_protected rd.immucore.write_protected=0")
		forced, _ := parseWriteProtected()
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
		setCmdline("rd.immucore.write_protected")
		answerProbe(false, nil)

		// A device that cannot exist, so a fsck that did run would fail. The
		// gate is what makes this nil. It sits before the mount in
		// MountOPWithFstab's PrepareCallback, so no mount option can stand in
		// for it.
		Expect(Fsck("/dev/immucore-does-not-exist")).To(Succeed())
	})

	It("skips a read-only device even when the global answer is forced off", func() {
		setCmdline("rd.immucore.write_protected=0")
		answerProbe(true, nil)
		Expect(Fsck("/dev/immucore-does-not-exist")).To(Succeed())
	})
})
