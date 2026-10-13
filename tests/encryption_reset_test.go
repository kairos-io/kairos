package mos_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/spectrocloud/peg/matcher"
)

// Reset parity for partition encryption (kairos-io/kairos#4556): a reset
// reformats the persistent partition, and when the configuration lists it in
// install.encrypted_partitions the reset has to encrypt it again, so a reset
// ends in the same state an install ends in. The audit trail the reset
// carries over the format must survive the re-encryption.

// encryptedPartitionsOnlyPolicy lists persistent as encrypted WITHOUT the
// kcrypt.encrypt_on_boot opt-in, so the boot time step stays out of the way
// and the only thing that can encrypt the partition is the reset under test.
const encryptedPartitionsOnlyPolicy = `#cloud-config

install:
  encrypted_partitions:
    - COS_PERSISTENT
`

// auditMarker is planted in the audit trail's backing directory on the
// persistent partition (agent constants AuditLogStatePath), which is what
// the reset stashes before the format and restores after it.
const auditMarker = "/usr/local/.state/var-log-audit.bind/reset-parity-marker"

var _ = Describe("kcrypt reset encryption parity", Label("encryption-reset-parity"), func() {
	var vm VM

	BeforeEach(func() {
		_, vm = startVM()
		vm.EventuallyConnects(1200)

		configFile, err := os.CreateTemp("", "")
		Expect(err).ToNot(HaveOccurred())
		defer os.Remove(configFile.Name())
		Expect(os.WriteFile(configFile.Name(), []byte(fmt.Sprintf(installConfigNoEncryptionFmt, "rd.immucore.debug")), 0744)).To(Succeed())
		Expect(vm.Scp(configFile.Name(), "/tmp/config.yaml", "0744")).To(Succeed())

		By("Manually installing without encryption")
		out, err := vm.Sudo("kairos-agent --debug manual-install --device auto /tmp/config.yaml")
		Expect(err).ToNot(HaveOccurred(), out)

		writeEncryptOnBootPolicy(vm, encryptedPartitionsOnlyPolicy)
	})

	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			serial, _ := os.ReadFile(filepath.Join(vm.StateDir, "serial.log"))
			_ = os.MkdirAll("logs", os.ModePerm|os.ModeDir)
			_ = os.WriteFile(filepath.Join("logs", "serial.log"), serial, os.ModePerm)
			fmt.Println(string(serial))
			gatherLogs(vm)
		}
		err := vm.Destroy(func(vm VM) {
			tpmPID, err := os.ReadFile(path.Join(vm.StateDir, "tpm", "pid"))
			Expect(err).ToNot(HaveOccurred())
			if len(tpmPID) != 0 {
				pid, err := strconv.Atoi(string(tpmPID))
				Expect(err).ToNot(HaveOccurred())
				syscall.Kill(pid, syscall.SIGKILL)
			}
		})
		Expect(err).ToNot(HaveOccurred())
	})

	It("re-encrypts persistent after the format and keeps the audit trail", func() {
		By("Booting the installed system")
		vm.Reboot()
		vm.EventuallyConnects(1200)

		By("Confirming persistent is still plaintext, since the boot time step is not opted in")
		out, err := vm.Sudo("blkid")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).ToNot(MatchRegexp("crypto_LUKS"), out)

		By("Planting data the reset must destroy and an audit trail it must keep")
		out, err = vm.Sudo("touch /usr/local/reset-must-remove && " +
			"mkdir -p " + path.Dir(auditMarker) + " && " +
			"echo audit > " + auditMarker + " && sync && echo PLANTED")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("PLANTED"), out)

		By("Resetting through the statereset boot entry")
		out, err = vm.Sudo("grub2-editenv /oem/grubenv set next_entry=statereset")
		Expect(err).ToNot(HaveOccurred(), out)
		vm.Reboot()
		expectRebootedToActive(vm)

		By("Checking the reset formatted persistent")
		out, _ = vm.Sudo("if [ -e /usr/local/reset-must-remove ]; then echo STILL_THERE; else echo GONE; fi")
		Expect(out).To(ContainSubstring("GONE"), out)

		By("Checking the reset encrypted persistent again")
		out, err = vm.Sudo("blkid")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(MatchRegexp("TYPE=\"crypto_LUKS\" PARTLABEL=\"persistent\""), out)
		Expect(out).To(MatchRegexp("/dev/(mapper|dm).*LABEL=\"COS_PERSISTENT\""), out)
		verifyPersistentFstabUsesMapper(vm)

		By("Checking the audit trail survived the re-encryption")
		out, err = vm.Sudo("cat " + auditMarker)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("audit"), out)
	})
})
