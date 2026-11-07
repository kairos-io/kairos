package action

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/kcrypt"
	"github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reset of a passphrase container left locked", func() {
	type wipeStub struct {
		asked    int
		answer   string
		askErr   error
		wiped    []string
		fsByPath map[string]string
	}

	// lockedPersistent is what the reset spec holds when recovery left the
	// container locked: the raw partition, found by its partition name,
	// with the container's label and filesystem.
	lockedPersistent := func() *partitions.Partition {
		return &partitions.Partition{
			Name: sdkConstants.PersistentPartName, Path: "/dev/vda5",
			FilesystemLabel: sdkConstants.PersistentLUKSLabel, FS: sdkConstants.LUKSFs,
		}
	}

	passphraseConfig := func(extra collector.ConfigValues) *config.Config {
		kc := collector.ConfigValues{"method": kcrypt.PassphraseMethod, "passphrase_file": "/run/hadron-install/passphrase"}
		for k, v := range extra {
			kc[k] = v
		}
		return &config.Config{
			Logger:    sdkLogger.NewNullLogger(),
			Install:   &install.Install{Encrypt: []string{sdkConstants.PersistentLabel}},
			Collector: collector.Config{Values: collector.ConfigValues{"kcrypt": kc}},
		}
	}

	stub := func(subsystem string) *wipeStub {
		s := &wipeStub{answer: "a new passphrase", fsByPath: map[string]string{"/dev/vda5": sdkConstants.LUKSFs}}
		origProbe, origSubsystem, origAsk := resetFsProbeFn, resetLUKSSubsystemFn, resetAskNewPassphraseFn
		origWipe, origFile, origUki := resetWipeSignaturesFn, resetPassphraseFile, resetIsUkiFn
		DeferCleanup(func() {
			resetFsProbeFn, resetLUKSSubsystemFn, resetAskNewPassphraseFn = origProbe, origSubsystem, origAsk
			resetWipeSignaturesFn, resetPassphraseFile, resetIsUkiFn = origWipe, origFile, origUki
		})
		resetFsProbeFn = func(path string) (string, error) { return s.fsByPath[path], nil }
		resetLUKSSubsystemFn = func(string) (string, error) { return subsystem, nil }
		resetAskNewPassphraseFn = func(string, time.Duration) (string, error) {
			s.asked++
			return s.answer, s.askErr
		}
		resetWipeSignaturesFn = func(_ *config.Config, device string) error {
			s.wiped = append(s.wiped, device)
			return nil
		}
		resetPassphraseFile = filepath.Join(GinkgoT().TempDir(), "reset", "passphrase")
		resetIsUkiFn = func() bool { return false }
		return s
	}

	It("asks for a new passphrase and prepares the format of a kcrypt:passphrase container", func() {
		s := stub(kcrypt.PassphraseSubsystem)
		cfg := passphraseConfig(nil)
		part := lockedPersistent()

		Expect(PreflightResetFormat(cfg, part, false)).To(Succeed())

		Expect(s.asked).To(Equal(1))
		Expect(part.FilesystemLabel).To(Equal(sdkConstants.PersistentLabel), "the format must write the plaintext label kcrypt looks for")
		Expect(part.FS).To(Equal(sdkConstants.LinuxFs), "mkfs cannot make a crypto_LUKS filesystem")
		Expect(cfg.Install.Encrypt).To(ContainElement(sdkConstants.PersistentLabel))

		kc := kcrypt.KcryptConfigFromCollector(&cfg.Collector, cfg.Logger)
		Expect(kc.Method).To(Equal(kcrypt.PassphraseMethod))
		Expect(kc.PassphraseFile).To(Equal(resetPassphraseFile))
		data, err := os.ReadFile(resetPassphraseFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal("a new passphrase"))
		info, err := os.Stat(resetPassphraseFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		Expect(s.wiped).To(BeEmpty(), "the preflight must not touch the disk")
	})

	It("uses kcrypt.passphrase_file without asking on an unattended reset", func() {
		s := stub(kcrypt.PassphraseSubsystem)
		file := filepath.Join(GinkgoT().TempDir(), "pass")
		Expect(os.WriteFile(file, []byte("unattended passphrase\n"), 0o600)).To(Succeed())
		cfg := passphraseConfig(collector.ConfigValues{"passphrase_file": file})

		Expect(PreflightResetFormat(cfg, lockedPersistent(), false)).To(Succeed())
		Expect(s.asked).To(Equal(0))
		Expect(kcrypt.KcryptConfigFromCollector(&cfg.Collector, cfg.Logger).PassphraseFile).To(Equal(file))
	})

	It("recreates a container whose header says passphrase even when the configuration does not", func() {
		stub(kcrypt.PassphraseSubsystem)
		cfg := &config.Config{Logger: sdkLogger.NewNullLogger()}

		Expect(PreflightResetFormat(cfg, lockedPersistent(), false)).To(Succeed())
		Expect(cfg.Install.Encrypt).To(ConsistOf(sdkConstants.PersistentLabel))
		Expect(kcrypt.KcryptConfigFromCollector(&cfg.Collector, cfg.Logger).Method).To(Equal(kcrypt.PassphraseMethod))
	})

	It("recreates a container with no subsystem when the configuration selects the passphrase method", func() {
		stub("")
		Expect(PreflightResetFormat(passphraseConfig(nil), lockedPersistent(), false)).To(Succeed())
	})

	It("keeps refusing a TPM, PCR or key server container", func() {
		s := stub("")
		cfg := &config.Config{Logger: sdkLogger.NewNullLogger(), Install: &install.Install{Encrypt: []string{sdkConstants.PersistentLabel}}}
		part := lockedPersistent()

		err := PreflightResetFormat(cfg, part, false)
		Expect(err).To(MatchError(ContainSubstring("raw LUKS container")))
		Expect(err).To(MatchError(ContainSubstring("nothing was formatted")))
		Expect(s.asked).To(Equal(0))
		Expect(part.FilesystemLabel).To(Equal(sdkConstants.PersistentLUKSLabel), "a refused partition must be left as it was")
	})

	It("refuses a container with another subsystem, even with the passphrase method configured", func() {
		stub("systemd-cryptenroll")
		Expect(PreflightResetFormat(passphraseConfig(nil), lockedPersistent(), false)).
			To(MatchError(ContainSubstring("raw LUKS container")))
	})

	It("refuses on a UKI node", func() {
		stub(kcrypt.PassphraseSubsystem)
		resetIsUkiFn = func() bool { return true }
		Expect(PreflightResetFormat(passphraseConfig(nil), lockedPersistent(), false)).
			To(MatchError(ContainSubstring("raw LUKS container")))
	})

	It("refuses when the header cannot be read", func() {
		stub(kcrypt.PassphraseSubsystem)
		resetLUKSSubsystemFn = func(string) (string, error) { return "", errors.New("blkid failed") }
		Expect(PreflightResetFormat(passphraseConfig(nil), lockedPersistent(), false)).
			To(MatchError(ContainSubstring("blkid failed")))
	})

	It("formats nothing when nobody types a new passphrase", func() {
		s := stub(kcrypt.PassphraseSubsystem)
		s.askErr = errors.New("timed out")
		part := lockedPersistent()

		err := PreflightResetFormat(passphraseConfig(nil), part, false)
		Expect(err).To(MatchError(ContainSubstring("needs a new passphrase")))
		Expect(err).To(MatchError(ContainSubstring("nothing was formatted")))
		Expect(part.FilesystemLabel).To(Equal(sdkConstants.PersistentLUKSLabel))
	})

	It("asks once when OEM and persistent are both recreated", func() {
		s := stub(kcrypt.PassphraseSubsystem)
		s.fsByPath["/dev/vda2"] = sdkConstants.LUKSFs
		cfg := passphraseConfig(nil)
		oem := &partitions.Partition{Name: sdkConstants.OEMPartName, Path: "/dev/vda2", FilesystemLabel: sdkConstants.OEMLUKSLabel, FS: sdkConstants.LUKSFs}

		Expect(PreflightResetFormats(cfg, lockedPersistent(), oem, true, true)).To(Succeed())
		Expect(s.asked).To(Equal(1))
		Expect(oem.FilesystemLabel).To(Equal(sdkConstants.OEMLabel))
		Expect(cfg.Install.Encrypt).To(ConsistOf(sdkConstants.PersistentLabel, sdkConstants.OEMLabel))
	})

	Describe("WipeLockedContainer", func() {
		It("wipes a raw LUKS container", func() {
			s := stub(kcrypt.PassphraseSubsystem)
			Expect(WipeLockedContainer(&config.Config{Logger: sdkLogger.NewNullLogger()}, lockedPersistent())).To(Succeed())
			Expect(s.wiped).To(Equal([]string{"/dev/vda5"}))
		})

		It("leaves a mapper and a plaintext partition alone", func() {
			s := stub(kcrypt.PassphraseSubsystem)
			s.fsByPath["/dev/vda5"] = "ext4"
			cfg := &config.Config{Logger: sdkLogger.NewNullLogger()}
			Expect(WipeLockedContainer(cfg, &partitions.Partition{Path: "/dev/mapper/vda5"})).To(Succeed())
			Expect(WipeLockedContainer(cfg, lockedPersistent())).To(Succeed())
			Expect(WipeLockedContainer(cfg, nil)).To(Succeed())
			Expect(s.wiped).To(BeEmpty())
		})

		It("runs wipefs on the device", func() {
			stub(kcrypt.PassphraseSubsystem)
			resetWipeSignaturesFn = origWipeSignatures
			runner := v1mock.NewFakeRunner()
			cfg := &config.Config{Logger: sdkLogger.NewNullLogger(), Runner: runner}
			Expect(WipeLockedContainer(cfg, lockedPersistent())).To(Succeed())
			Expect(runner.IncludesCmds([][]string{{"wipefs", "--all", "/dev/vda5"}})).To(Succeed())
		})
	})
})

// origWipeSignatures is the real wipefs call, kept for the spec that checks
// the command line through the fake runner.
var origWipeSignatures = resetWipeSignaturesFn
