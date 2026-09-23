package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("encrypt pending partitions", func() {
	// mockDisk stages a single-disk ghw view for the pending-encryption
	// lookups and cleans it up with the spec.
	mockDisk := func(parts partitions.PartitionList) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{Name: "vda", Partitions: parts})
		ghwMock.CreateDevices()
		DeferCleanup(ghwMock.Clean)
	}

	// mockCmdline points the cmdline readers at a file of our making, so
	// GetOemLabel does not read the test host's /proc/cmdline.
	mockCmdline := func(content string) {
		cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmdline, []byte(content), 0o600)).To(Succeed())
		old, had := os.LookupEnv("HOST_PROC_CMDLINE")
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv("HOST_PROC_CMDLINE", old)).To(Succeed())
				return
			}
			Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
		})
	}

	// stubProbes replaces the device probes that need a real udev and blkid
	// (absent on a test host) and shortens the retry schedule. The lookups
	// themselves still run against the ghw mock.
	stubProbes := func() {
		origSettle, origBlkid, origProbe := udevSettleFn, blkidLookupFn, filesystemProbeFn
		origAttempts, origBase := encryptPendingLookupAttempts, encryptPendingRetryBase
		DeferCleanup(func() {
			udevSettleFn, blkidLookupFn, filesystemProbeFn = origSettle, origBlkid, origProbe
			encryptPendingLookupAttempts, encryptPendingRetryBase = origAttempts, origBase
		})
		udevSettleFn = func() error { return nil }
		blkidLookupFn = func(string) (*partitions.Partition, error) { return nil, errors.New("blkid: not found") }
		filesystemProbeFn = func(string) (string, error) { return "", errors.New("blkid unavailable") }
		encryptPendingLookupAttempts = 2
		encryptPendingRetryBase = 0
	}

	// policyConfig builds the collector view of an encrypt-on-boot opt-in.
	policyConfig := func(enabled bool, labels ...string) *collector.Config {
		values := collector.ConfigValues{
			"kcrypt": collector.ConfigValues{"encrypt_on_boot": enabled},
		}
		if len(labels) > 0 {
			list := make([]interface{}, len(labels))
			for i, l := range labels {
				list[i] = l
			}
			values["install"] = collector.ConfigValues{"encrypted_partitions": list}
		}
		return &collector.Config{Values: values}
	}

	type runStub struct {
		encrypted [][]string
		halts     int
	}

	// stubRun additionally replaces the hooks runEncryptPending calls out
	// through (config scan, encryptor, halt screen) and returns what the
	// spec reads back: the labels handed to the encryptor and the number of
	// halt screens painted.
	stubRun := func(config *collector.Config, scanErr, encryptErr error) *runStub {
		stubProbes()
		mockCmdline("root=LABEL=COS_ACTIVE rd.immucore.debug\n")
		origScan, origEncrypt, origHalt := scanEncryptOnBootConfigFn, encryptPendingFn, haltWithBannerFn
		DeferCleanup(func() {
			scanEncryptOnBootConfigFn, encryptPendingFn, haltWithBannerFn = origScan, origEncrypt, origHalt
		})

		stub := &runStub{}
		scanEncryptOnBootConfigFn = func() (*collector.Config, error) { return config, scanErr }
		encryptPendingFn = func(_ *collector.Config, labels []string) error {
			stub.encrypted = append(stub.encrypted, labels)
			return encryptErr
		}
		haltWithBannerFn = func(_, _ string, _ error) { stub.halts++ }
		return stub
	}

	Describe("pendingEncryptionLabels", func() {
		BeforeEach(stubProbes)

		It("reports a plaintext partition as pending", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: "ext4"},
			})
			Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).
				To(Equal([]string{constants.PersistentLabel}))
		})

		It("skips a LUKS container carrying the outer label", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLUKSLabel, FS: constants.LUKSFs},
			})
			Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).To(BeEmpty())
		})

		It("skips a LUKS container sharing the plaintext label", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: constants.LUKSFs},
			})
			Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).To(BeEmpty())
		})

		It("errors on a label that is nowhere on disk", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda1", PartitionLabel: "efi", FilesystemLabel: "COS_GRUB", FS: "vfat"},
			})
			_, err := pendingEncryptionLabels([]string{constants.PersistentLabel})
			Expect(err).To(MatchError(ContainSubstring("was not found")))
		})

		It("reports only the plaintext labels of a mixed set", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda4", PartitionLabel: "data", FilesystemLabel: "MYAPP_DATA_LUKS", FS: constants.LUKSFs},
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: "ext4"},
			})
			Expect(pendingEncryptionLabels([]string{"MYAPP_DATA", constants.PersistentLabel})).
				To(Equal([]string{constants.PersistentLabel}))
		})

		It("fails the settle rather than classifying against an unsettled udev", func() {
			udevSettleFn = func() error { return errors.New("udevadm settle timed out") }
			_, err := pendingEncryptionLabels([]string{constants.PersistentLabel})
			Expect(err).To(MatchError(ContainSubstring("settle")))
		})

		Context("when udev has not recorded a filesystem type", func() {
			It("skips the partition once blkid says it is LUKS", func() {
				mockDisk(partitions.PartitionList{
					{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: ""},
				})
				filesystemProbeFn = func(string) (string, error) { return constants.LUKSFs, nil }
				Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).To(BeEmpty())
			})

			It("reports the partition pending once blkid says it is plaintext", func() {
				mockDisk(partitions.PartitionList{
					{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: ""},
				})
				filesystemProbeFn = func(string) (string, error) { return "ext4", nil }
				Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).
					To(Equal([]string{constants.PersistentLabel}))
			})

			It("refuses to treat an undeterminable filesystem as plaintext", func() {
				mockDisk(partitions.PartitionList{
					{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: ""},
				})
				_, err := pendingEncryptionLabels([]string{constants.PersistentLabel})
				Expect(err).To(MatchError(ContainSubstring("could not be determined")))
			})
		})

		Context("when only blkid can see the partition (pre kairos-sdk#822 installs)", func() {
			BeforeEach(func() {
				mockDisk(partitions.PartitionList{
					{Name: "vda1", PartitionLabel: "efi", FilesystemLabel: "COS_GRUB", FS: "vfat"},
				})
				blkidLookupFn = func(string) (*partitions.Partition, error) {
					return &partitions.Partition{Name: "vda5", Path: "/dev/vda5"}, nil
				}
			})

			It("skips a legacy LUKS container instead of halting the boot", func() {
				filesystemProbeFn = func(string) (string, error) { return constants.LUKSFs, nil }
				Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).To(BeEmpty())
			})

			It("reports a legacy plaintext partition as pending", func() {
				filesystemProbeFn = func(string) (string, error) { return "ext4", nil }
				Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).
					To(Equal([]string{constants.PersistentLabel}))
			})
		})

		It("retries the classification before giving up", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda1", PartitionLabel: "efi", FilesystemLabel: "COS_GRUB", FS: "vfat"},
			})
			calls := 0
			blkidLookupFn = func(string) (*partitions.Partition, error) {
				calls++
				if calls == 1 {
					return nil, errors.New("udev still probing")
				}
				return &partitions.Partition{Name: "vda5", Path: "/dev/vda5"}, nil
			}
			filesystemProbeFn = func(string) (string, error) { return "ext4", nil }
			Expect(pendingEncryptionLabels([]string{constants.PersistentLabel})).
				To(Equal([]string{constants.PersistentLabel}))
			Expect(calls).To(Equal(2))
		})
	})

	Describe("runEncryptPending", func() {
		ctx := context.Background()
		s := &State{}

		It("is a no-op without the opt-in", func() {
			stub := stubRun(policyConfig(false, constants.PersistentLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(BeZero())
		})

		It("is a no-op with an empty partition list", func() {
			stub := stubRun(policyConfig(true), nil, nil)
			Expect(s.runEncryptPending(ctx)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(BeZero())
		})

		It("is a no-op when everything is already encrypted", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLUKSLabel, FS: constants.LUKSFs},
			})
			stub := stubRun(policyConfig(true, constants.PersistentLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).To(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(BeZero())
		})

		It("encrypts a pending partition", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: "ext4"},
			})
			stub := stubRun(policyConfig(true, constants.PersistentLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).To(Succeed())
			Expect(stub.encrypted).To(Equal([][]string{{constants.PersistentLabel}}))
			Expect(stub.halts).To(BeZero())
		})

		It("halts when the configuration cannot be read", func() {
			stub := stubRun(nil, errors.New("scanning configuration: I/O error"), nil)
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(Equal(1))
		})

		It("halts on a pending OEM partition", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda3", PartitionLabel: "oem", FilesystemLabel: constants.OEMLabel, FS: "ext4"},
			})
			stub := stubRun(policyConfig(true, constants.OEMLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(Equal(1))
		})

		It("halts on a pending OEM partition carrying a custom label", func() {
			stub := stubRun(policyConfig(true, "MY_OEM"), nil, nil)
			// After stubRun: its default cmdline must lose to this one.
			mockCmdline("root=LABEL=COS_ACTIVE rd.immucore.oemlabel=MY_OEM\n")
			mockDisk(partitions.PartitionList{
				{Name: "vda3", PartitionLabel: "oem", FilesystemLabel: "MY_OEM", FS: "ext4"},
			})
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(Equal(1))
		})

		It("halts on a pending state partition", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda4", PartitionLabel: "state", FilesystemLabel: constants.StateLabel, FS: "ext4"},
			})
			stub := stubRun(policyConfig(true, constants.StateLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(Equal(1))
		})

		It("halts on a pending recovery partition", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda4", PartitionLabel: "recovery", FilesystemLabel: constants.RecoveryLabel, FS: "ext4"},
			})
			stub := stubRun(policyConfig(true, constants.RecoveryLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(Equal(1))
		})

		It("halts when a configured partition is missing", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda1", PartitionLabel: "efi", FilesystemLabel: "COS_GRUB", FS: "vfat"},
			})
			stub := stubRun(policyConfig(true, constants.PersistentLabel), nil, nil)
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(BeEmpty())
			Expect(stub.halts).To(Equal(1))
		})

		It("halts when encryption fails", func() {
			mockDisk(partitions.PartitionList{
				{Name: "vda5", PartitionLabel: "persistent", FilesystemLabel: constants.PersistentLabel, FS: "ext4"},
			})
			stub := stubRun(policyConfig(true, constants.PersistentLabel), nil, errors.New("no TPM device"))
			Expect(s.runEncryptPending(ctx)).ToNot(Succeed())
			Expect(stub.encrypted).To(HaveLen(1))
			Expect(stub.halts).To(Equal(1))
		})
	})
})
