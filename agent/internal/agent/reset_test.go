package agent

import (
	"os"
	"path/filepath"
	"time"

	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("scanResetConfig", func() {
	var dir string

	BeforeEach(func() {
		var err error
		dir, err = os.MkdirTemp("", "reset-strict")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { os.RemoveAll(dir) })

		// install.device only accepts "auto", a /dev path or a script:// url,
		// so "sda" is a schema error the scan has to decide what to do with.
		err = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(
			"#cloud-config\ninstall:\n  device: sda\n"), 0644)
		Expect(err).ToNot(HaveOccurred())
	})

	It("fails on a config that does not match the schema when strict validation is on", func() {
		_, err := scanResetConfig("{}", true, dir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("device"))
	})

	It("warns and carries on when strict validation is off", func() {
		c, err := scanResetConfig("{}", false, dir)
		Expect(err).ToNot(HaveOccurred())
		Expect(c.Install.Device).To(Equal("sda"))
	})
})

var _ = Describe("reset on a node without a recovery partition", func() {
	withRecoveryPartition := func(p *sdkPartitions.Partition) {
		orig := recoveryPartition
		recoveryPartition = func() *sdkPartitions.Partition { return p }
		DeferCleanup(func() { recoveryPartition = orig })
	}

	It("refuses before the confirmation wait", func() {
		withRecoveryPartition(nil)

		start := time.Now()
		// Interactive on purpose: without the guard this sits in the 60 second wait.
		err := reset(false, false, false, false)

		Expect(err).To(MatchError(ContainSubstring("This node has no recovery partition")))
		Expect(err).To(MatchError(ContainSubstring("Replace the node with a fresh disk")))
		Expect(time.Since(start)).To(BeNumerically("<", 5*time.Second))
	})

	It("does not refuse a node that has a recovery partition", func() {
		withRecoveryPartition(&sdkPartitions.Partition{FilesystemLabel: "COS_RECOVERY"})

		// Unattended skips the wait. The reset then fails on the test machine for
		// its own reasons, which must not be the no recovery refusal.
		err := reset(false, true, false, false)

		if err != nil {
			Expect(err.Error()).ToNot(ContainSubstring("no recovery partition"))
		}
	})
})
