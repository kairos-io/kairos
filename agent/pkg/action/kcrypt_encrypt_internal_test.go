package action

import (
	"errors"
	"os"
	"path/filepath"

	v1 "github.com/kairos-io/kairos/v4/agent/pkg/implementations/spec"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
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
		DeferCleanup(func() {
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

	Describe("encryptFormattedPersistent", func() {
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
			Expect(r.encryptFormattedPersistent(persistent)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(persistent.Path).To(Equal("/dev/vda5"))
		})

		It("is a no-op on a nil partition", func() {
			stub := stubAll()
			r, _ := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			Expect(r.encryptFormattedPersistent(nil)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("is a no-op when the format preserved the LUKS container", func() {
			stub := stubAll()
			stub.disksNow = luksPersistent
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			Expect(r.encryptFormattedPersistent(persistent)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("encrypts, unlocks and repoints the spec at the mapper", func() {
			stub := stubAll()
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			Expect(r.encryptFormattedPersistent(persistent)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
			Expect(stub.unlocked).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
			Expect(persistent.Path).To(Equal("/dev/mapper/vda5"))
		})

		It("fails the reset when encryption fails", func() {
			stubAll()
			kcryptEncryptFn = func(*config.Config, []string) error { return errors.New("no TPM device") }
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			err := r.encryptFormattedPersistent(persistent)
			Expect(err).To(MatchError(ContainSubstring("no TPM device")))
			Expect(persistent.Path).To(Equal("/dev/vda5"))
		})

		It("fails the reset when the unlock after encryption fails", func() {
			stubAll()
			kcryptUnlockFn = func(*config.Config, []string) error { return errors.New("unlock failed") }
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			err := r.encryptFormattedPersistent(persistent)
			Expect(err).To(MatchError(ContainSubstring("unlock failed")))
		})

		It("fails the reset when the partition cannot be classified", func() {
			stub := stubAll()
			stub.disksNow = func() []*partitions.Disk { return disksWith() }
			r, persistent := newReset(configWithEncrypt(sdkConstants.PersistentLabel))
			err := r.encryptFormattedPersistent(persistent)
			Expect(err).To(MatchError(ContainSubstring("was not found")))
			Expect(stub.encrypted).To(BeEmpty())
		})

		It("applies the UKI default when no partitions are listed", func() {
			stub := stubAll()
			resetIsUkiFn = func() bool { return true }
			r, persistent := newReset(newConfig())
			Expect(r.encryptFormattedPersistent(persistent)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{sdkConstants.PersistentLabel}}))
		})
	})
})
