package action

import (
	"errors"
	"os"
	"path/filepath"

	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	"github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("kcrypt encrypt", func() {
	// encryptStub records what the stubbed hooks were asked to do and what
	// they should answer. The disks view is swapped between calls through
	// disksNow so the post-encrypt verification can see a different world
	// than the classification did.
	type encryptStub struct {
		encrypted  [][]string
		unlocked   [][]string
		confirms   int
		confirmAns bool
		disksNow   func() []*partitions.Disk
	}

	newConfig := func() *config.Config {
		return &config.Config{Logger: sdkLogger.NewNullLogger()}
	}

	disksWith := func(parts ...*partitions.Partition) []*partitions.Disk {
		return []*partitions.Disk{{Name: "vda", Partitions: parts}}
	}
	plaintextPersistent := func() []*partitions.Disk {
		return disksWith(&partitions.Partition{
			Name: "vda5", Path: "/dev/vda5",
			FilesystemLabel: sdkConstants.PersistentLabel, FS: "ext4",
		})
	}
	luksPersistent := func() []*partitions.Disk {
		return disksWith(&partitions.Partition{
			Name: "vda5", Path: "/dev/vda5",
			FilesystemLabel: sdkConstants.PersistentLUKSLabel, FS: sdkConstants.LUKSFs,
		})
	}

	// stubAll replaces every package seam and restores it with the spec. By
	// default the disks show a plaintext persistent partition, nothing is
	// mounted, encryption succeeds and flips the disks view to LUKS, and the
	// prompt confirms.
	stubAll := func() *encryptStub {
		origScan, origBlkid, origProbe := kcryptScanDisksFn, kcryptBlkidLookupFn, kcryptFsProbeFn
		origSettle, origEncrypt, origUnlock := kcryptUdevSettleFn, kcryptEncryptFn, kcryptUnlockFn
		origMounts, origConfirm, origCmdline := kcryptMountpointsFn, kcryptConfirmFn, procCmdlinePath
		origIsUki, origMountSource := resetIsUkiFn, resetMountSourceFn
		origResetProbe, origResetEncryptor := resetFsProbeFn, resetEncryptorFn
		DeferCleanup(func() {
			resetFsProbeFn, resetEncryptorFn = origResetProbe, origResetEncryptor
			kcryptScanDisksFn, kcryptBlkidLookupFn, kcryptFsProbeFn = origScan, origBlkid, origProbe
			kcryptUdevSettleFn, kcryptEncryptFn, kcryptUnlockFn = origSettle, origEncrypt, origUnlock
			kcryptMountpointsFn, kcryptConfirmFn, procCmdlinePath = origMounts, origConfirm, origCmdline
			resetIsUkiFn, resetMountSourceFn = origIsUki, origMountSource
		})

		stub := &encryptStub{confirmAns: true}
		stub.disksNow = plaintextPersistent

		kcryptScanDisksFn = func() ([]*partitions.Disk, error) { return stub.disksNow(), nil }
		kcryptBlkidLookupFn = func(string) (*partitions.Partition, error) { return nil, errors.New("blkid: not found") }
		kcryptFsProbeFn = func(string) (string, error) { return "", errors.New("blkid unavailable") }
		kcryptUdevSettleFn = func(*config.Config) error { return nil }
		kcryptEncryptFn = func(_ *config.Config, labels []string) error {
			stub.encrypted = append(stub.encrypted, labels)
			stub.disksNow = luksPersistent
			return nil
		}
		kcryptUnlockFn = func(_ *config.Config, labels []string) error {
			stub.unlocked = append(stub.unlocked, labels)
			return nil
		}
		kcryptMountpointsFn = func(string) ([]string, error) { return nil, nil }
		kcryptConfirmFn = func() bool {
			stub.confirms++
			return stub.confirmAns
		}
		resetIsUkiFn = func() bool { return false }
		resetMountSourceFn = func(string) (string, error) { return "/dev/mapper/vda5", nil }
		resetFsProbeFn = func(string) (string, error) { return "ext4", nil }
		resetEncryptorFn = func(*config.Config) (kcrypt.PartitionEncryptor, error) {
			return &kcrypt.LocalTPMNVEncryptor{}, nil
		}

		// No cmdline OEM rename unless a spec writes one.
		cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_ACTIVE\n"), 0o600)).To(Succeed())
		procCmdlinePath = cmdline
		return stub
	}

	Describe("KcryptEncrypt", func() {
		It("refuses to run without labels", func() {
			stubAll()
			err := KcryptEncrypt(newConfig(), []string{"", "  "}, true)
			Expect(err).To(MatchError(ContainSubstring("no partition labels")))
		})

		DescribeTable("refuses the partitions the running system depends on",
			func(label string) {
				stub := stubAll()
				err := KcryptEncrypt(newConfig(), []string{label}, true)
				Expect(err).To(MatchError(ContainSubstring("is not supported")))
				Expect(stub.encrypted).To(BeEmpty())
			},
			Entry("OEM", sdkConstants.OEMLabel),
			Entry("state", sdkConstants.StateLabel),
			Entry("recovery", sdkConstants.RecoveryLabel),
			Entry("EFI", sdkConstants.EfiLabel),
		)

		It("refuses a renamed OEM read from the cmdline", func() {
			stub := stubAll()
			cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
			Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_ACTIVE rd.immucore.oemlabel=MY_OEM\n"), 0o600)).To(Succeed())
			procCmdlinePath = cmdline

			err := KcryptEncrypt(newConfig(), []string{"MY_OEM"}, true)
			Expect(err).To(MatchError(ContainSubstring("is not supported")))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("skips a partition that is already a LUKS container", func() {
			stub := stubAll()
			stub.disksNow = luksPersistent
			Expect(KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("fails closed on a label that cannot be found", func() {
			stub := stubAll()
			stub.disksNow = func() []*partitions.Disk { return disksWith() }
			err := KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)
			Expect(err).To(MatchError(ContainSubstring("was not found")))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("refuses a mounted partition", func() {
			stub := stubAll()
			kcryptMountpointsFn = func(string) ([]string, error) { return []string{"/usr/local"}, nil }
			err := KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)
			Expect(err).To(MatchError(ContainSubstring("unmount it first")))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("refuses when whether the partition is mounted cannot be determined", func() {
			stub := stubAll()
			kcryptMountpointsFn = func(string) ([]string, error) {
				return nil, errors.New("cannot resolve the device")
			}
			err := KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)
			Expect(err).To(MatchError(ContainSubstring("checking whether COS_PERSISTENT is mounted")))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("refuses a partition mounted while the confirmation prompt was open", func() {
			stub := stubAll()
			mounted := false
			kcryptMountpointsFn = func(string) ([]string, error) {
				if mounted {
					return []string{"/usr/local"}, nil
				}
				return nil, nil
			}
			kcryptConfirmFn = func() bool {
				stub.confirms++
				mounted = true
				return true
			}
			err := KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, false)
			Expect(err).To(MatchError(ContainSubstring("unmount it first")))
			Expect(stub.confirms).To(Equal(1))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("does nothing when the prompt is declined", func() {
			stub := stubAll()
			stub.confirmAns = false
			Expect(KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, false)).To(Succeed())
			Expect(stub.confirms).To(Equal(1))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("does not prompt when the confirmation is waived", func() {
			stub := stubAll()
			Expect(KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)).To(Succeed())
			Expect(stub.confirms).To(BeZero())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
		})

		It("encrypts only the pending labels and verifies the result", func() {
			stub := stubAll()
			mixed := func() []*partitions.Disk {
				return disksWith(
					&partitions.Partition{Name: "vda4", Path: "/dev/vda4", FilesystemLabel: "MYAPP_DATA_LUKS", FS: sdkConstants.LUKSFs},
					&partitions.Partition{Name: "vda5", Path: "/dev/vda5", FilesystemLabel: sdkConstants.PersistentLabel, FS: "ext4"},
				)
			}
			verified := func() []*partitions.Disk {
				return disksWith(
					&partitions.Partition{Name: "vda4", Path: "/dev/vda4", FilesystemLabel: "MYAPP_DATA_LUKS", FS: sdkConstants.LUKSFs},
					&partitions.Partition{Name: "vda5", Path: "/dev/vda5", FilesystemLabel: sdkConstants.PersistentLUKSLabel, FS: sdkConstants.LUKSFs},
				)
			}
			stub.disksNow = mixed
			kcryptEncryptFn = func(_ *config.Config, labels []string) error {
				stub.encrypted = append(stub.encrypted, labels)
				stub.disksNow = verified
				return nil
			}

			Expect(KcryptEncrypt(newConfig(), []string{"MYAPP_DATA", sdkConstants.PersistentLabel}, true)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
		})

		It("errors when the result does not verify as encrypted", func() {
			stub := stubAll()
			kcryptEncryptFn = func(_ *config.Config, labels []string) error {
				stub.encrypted = append(stub.encrypted, labels)
				// The disks still show plaintext after the "successful" encryption.
				return nil
			}
			err := KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)
			Expect(err).To(MatchError(ContainSubstring("does not verify")))
		})

		It("propagates an encryption failure", func() {
			stubAll()
			kcryptEncryptFn = func(*config.Config, []string) error { return errors.New("no TPM device") }
			err := KcryptEncrypt(newConfig(), []string{sdkConstants.PersistentLabel}, true)
			Expect(err).To(MatchError(ContainSubstring("no TPM device")))
		})
	})

	Describe("EncryptFormattedPartition", func() {
		newReset := func(cfg *config.Config) (*ResetAction, *partitions.Partition) {
			persistent := &partitions.Partition{
				Name: "vda5", Path: "/dev/vda5",
				FilesystemLabel: sdkConstants.PersistentLabel,
			}
			r := &ResetAction{cfg: cfg, spec: &v1.ResetSpec{}}
			return r, persistent
		}
		configWithEncrypt := func(labels ...string) *config.Config {
			cfg := newConfig()
			cfg.Install = &install.Install{Encrypt: labels}
			return cfg
		}

		It("is a no-op when nothing is configured for encryption", func() {
			stub := stubAll()
			r, persistent := newReset(newConfig())
			Expect(EncryptFormattedPartition(r.cfg, persistent)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(persistent.Path).To(Equal("/dev/vda5"))
		})

		It("is a no-op on a nil partition", func() {
			stub := stubAll()
			r, _ := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			Expect(EncryptFormattedPartition(r.cfg, nil)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("is a no-op when the format preserved the LUKS container", func() {
			stub := stubAll()
			stub.disksNow = luksPersistent
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			Expect(EncryptFormattedPartition(r.cfg, persistent)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("encrypts, unlocks and repoints the spec at the mapper", func() {
			stub := stubAll()
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			Expect(EncryptFormattedPartition(r.cfg, persistent)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
			Expect(stub.unlocked).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
			Expect(persistent.Path).To(Equal("/dev/mapper/vda5"))
		})

		It("fails the reset when encryption fails", func() {
			stubAll()
			kcryptEncryptFn = func(*config.Config, []string) error { return errors.New("no TPM device") }
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			err := EncryptFormattedPartition(r.cfg, persistent)
			Expect(err).To(MatchError(ContainSubstring("no TPM device")))
			Expect(persistent.Path).To(Equal("/dev/vda5"))
		})

		It("fails the reset when the unlock after encryption fails", func() {
			stubAll()
			kcryptUnlockFn = func(*config.Config, []string) error { return errors.New("unlock failed") }
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			err := EncryptFormattedPartition(r.cfg, persistent)
			Expect(err).To(MatchError(ContainSubstring("unlock failed")))
		})

		It("fails the reset when the partition cannot be classified", func() {
			stub := stubAll()
			stub.disksNow = func() []*partitions.Disk { return disksWith() }
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			err := EncryptFormattedPartition(r.cfg, persistent)
			Expect(err).To(MatchError(ContainSubstring("was not found")))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("applies the UKI default when no partitions are listed", func() {
			stub := stubAll()
			resetIsUkiFn = func() bool { return true }
			r, persistent := newReset(newConfig())
			Expect(EncryptFormattedPartition(r.cfg, persistent)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
		})

		It("encrypts a freshly formatted OEM partition when it is listed", func() {
			stub := stubAll()
			stub.disksNow = func() []*partitions.Disk {
				return disksWith(&partitions.Partition{
					Name: "vda2", Path: "/dev/vda2",
					FilesystemLabel: sdkConstants.OEMLabel, FS: "ext4",
				})
			}
			r, _ := newReset(configWithEncrypt(sdkConstants.OEMLabel))
			oem := &partitions.Partition{Name: "vda2", Path: "/dev/vda2", FilesystemLabel: sdkConstants.OEMLabel}
			Expect(EncryptFormattedPartition(r.cfg, oem)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.OEMLabel}}))
			Expect(stub.unlocked).To(Equal([][]string{{sdkConstants.OEMLabel}}))
			Expect(oem.Path).To(Equal("/dev/mapper/vda5"), "the spec must point at the mapper the stub resolved")
		})

		It("applies the UKI default to OEM as well", func() {
			stub := stubAll()
			resetIsUkiFn = func() bool { return true }
			stub.disksNow = func() []*partitions.Disk {
				return disksWith(&partitions.Partition{
					Name: "vda2", Path: "/dev/vda2",
					FilesystemLabel: sdkConstants.OEMLabel, FS: "ext4",
				})
			}
			r, _ := newReset(newConfig())
			oem := &partitions.Partition{Name: "vda2", Path: "/dev/vda2", FilesystemLabel: sdkConstants.OEMLabel}
			Expect(EncryptFormattedPartition(r.cfg, oem)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.OEMLabel}}))
		})
	})

	Describe("PreflightResetFormat", func() {
		persistentAt := func(path string) *partitions.Partition {
			return &partitions.Partition{Name: "vda5", Path: path, FilesystemLabel: sdkConstants.PersistentLabel}
		}
		configWithEncrypt := func(labels ...string) *config.Config {
			cfg := newConfig()
			cfg.Install = &install.Install{Encrypt: labels}
			return cfg
		}

		It("passes a partition on its mapper without probing it", func() {
			stubAll()
			resetFsProbeFn = func(string) (string, error) {
				Fail("a mapper path must not be probed")
				return "", nil
			}
			Expect(PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/mapper/vda5"), false)).To(Succeed())
		})

		It("refuses a raw LUKS container, configured or not", func() {
			stubAll()
			resetFsProbeFn = func(string) (string, error) { return sdkConstants.LUKSFs, nil }
			Expect(PreflightResetFormat(newConfig(), persistentAt("/dev/vda5"), false)).
				To(MatchError(ContainSubstring("raw LUKS container")))
			Expect(PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/vda5"), false)).
				To(MatchError(ContainSubstring("raw LUKS container")))
		})

		It("passes a plaintext partition nothing asks to encrypt", func() {
			stubAll()
			resetEncryptorFn = func(*config.Config) (kcrypt.PartitionEncryptor, error) {
				Fail("the encryptor must not be built when nothing is configured")
				return nil, nil
			}
			Expect(PreflightResetFormat(newConfig(), persistentAt("/dev/vda5"), false)).To(Succeed())
		})

		It("refuses before any format when the encryptor cannot be validated", func() {
			stubAll()
			resetEncryptorFn = func(*config.Config) (kcrypt.PartitionEncryptor, error) {
				return nil, errors.New("could not find TPM 2.0 device")
			}
			err := PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/vda5"), false)
			Expect(err).To(MatchError(ContainSubstring("could not find TPM 2.0 device")))
			Expect(err).To(MatchError(ContainSubstring("nothing was formatted")))
		})

		It("fails closed when the filesystem of a configured partition cannot be determined", func() {
			stubAll()
			resetFsProbeFn = func(string) (string, error) { return "", errors.New("blkid failed") }
			Expect(PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/vda5"), false)).
				To(MatchError(ContainSubstring("cannot determine the filesystem")))
		})

		It("keeps an unconfigured reset going when the probe fails", func() {
			stubAll()
			resetFsProbeFn = func(string) (string, error) { return "", errors.New("blkid failed") }
			Expect(PreflightResetFormat(newConfig(), persistentAt("/dev/vda5"), false)).To(Succeed())
		})

		It("refuses remote KMS re-encryption when OEM is formatted too", func() {
			stubAll()
			resetEncryptorFn = func(*config.Config) (kcrypt.PartitionEncryptor, error) {
				return &kcrypt.RemoteKMSEncryptor{}, nil
			}
			Expect(PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/vda5"), true)).
				To(MatchError(ContainSubstring("remote KMS")))
			Expect(PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/vda5"), false)).
				To(Succeed(), "remote KMS without an OEM format is allowed")
		})

		It("allows local TPM re-encryption when OEM is formatted too", func() {
			stubAll()
			Expect(PreflightResetFormat(configWithEncrypt(sdkConstants.PersistentLabel), persistentAt("/dev/vda5"), true)).To(Succeed())
		})
	})
})

var _ = Describe("mountpointsForDevice", func() {
	writeTable := func(content string) string {
		p := filepath.Join(GinkgoT().TempDir(), "mounts")
		Expect(os.WriteFile(p, []byte(content), 0o600)).To(Succeed())
		return p
	}

	It("lists every mountpoint of the device, bind mounts included", func() {
		table := writeTable("/dev/vda5 /usr/local ext4 rw 0 0\n" +
			"/dev/vda5 /var/log ext4 rw 0 0\n" +
			"/dev/vda2 /oem ext4 rw 0 0\n")
		Expect(mountpointsForDevice(table, "/dev/vda5")).To(Equal([]string{"/usr/local", "/var/log"}))
	})

	It("reports an unmounted device as having no mountpoints", func() {
		table := writeTable("/dev/vda2 /oem ext4 rw 0 0\n")
		Expect(mountpointsForDevice(table, "/dev/vda5")).To(BeEmpty())
	})

	It("unescapes spaces in mountpoints", func() {
		table := writeTable(`/dev/vda5 /mnt/my\040data ext4 rw 0 0` + "\n")
		Expect(mountpointsForDevice(table, "/dev/vda5")).To(Equal([]string{"/mnt/my data"}))
	})

	It("fails closed when the mount table cannot be read", func() {
		_, err := mountpointsForDevice(filepath.Join(GinkgoT().TempDir(), "missing"), "/dev/vda5")
		Expect(err).To(MatchError(ContainSubstring("reading the mount table")))
	})
})
