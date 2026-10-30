package dag_test

import (
	"os"
	"path/filepath"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/dag"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("the in-RAM boot dag", func() {
	// mockOEM stages a single disk whose OEM partition carries the given
	// filesystem, which is what oemEncrypted() reads to pick the branch.
	mockOEM := func(fs string) {
		var ghwMock mocks.GhwMock
		ghwMock.AddDisk(partitions.Disk{Name: "vda", Partitions: partitions.PartitionList{
			{Name: "vda2", PartitionLabel: "oem", FilesystemLabel: sdkConstants.OEMLabel, FS: fs},
		}})
		ghwMock.CreateDevices()
		DeferCleanup(ghwMock.Clean)
	}

	// inRAMSteps builds the in-RAM graph and returns its steps by name.
	inRAMSteps := func() map[string]herd.GraphEntry {
		g := herd.DAG(herd.EnableInit)
		Expect(dag.RegisterInRAMBoot(&state.State{Rootdir: "/sysroot", InRAM: true}, g)).To(Succeed())
		return entries(g)
	}

	BeforeEach(func() {
		internalUtils.KLog = logger.NewKairosLogger("immucore-test", "error", true)

		// Keep GetOemLabel off the test host's own /proc/cmdline.
		cmdline := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmdline, []byte("kairos.ram\n"), 0o600)).To(Succeed())
		old, had := os.LookupEnv("HOST_PROC_CMDLINE")
		Expect(os.Setenv("HOST_PROC_CMDLINE", cmdline)).To(Succeed())
		DeferCleanup(func() {
			if had {
				Expect(os.Setenv("HOST_PROC_CMDLINE", old)).To(Succeed())
				return
			}
			Expect(os.Unsetenv("HOST_PROC_CMDLINE")).To(Succeed())
		})
	})

	Context("on a node whose OEM is plaintext", func() {
		BeforeEach(func() { mockOEM("ext4") })

		It("registers the encrypt-pending step", func() {
			// Without it kcrypt.encrypt_on_boot does nothing on this path,
			// and says nothing either: the no-op log line belongs to the
			// step that was never added. ensure-partitions only encrypts
			// what it creates under UKI, so on a GRUB in-RAM boot this is
			// the only step that can encrypt a pending partition
			// (kairos-io/kairos#5410).
			Expect(inRAMSteps()).To(HaveKey(cnst.OpEncryptPending))
		})

		It("encrypts before the unlock opens the partitions", func() {
			steps := inRAMSteps()
			Expect(steps[cnst.OpKcryptUnlock].Dependencies).To(ContainElement(cnst.OpEncryptPending))
			Expect(steps[cnst.OpKcryptUnlock].WeakDependencies).NotTo(ContainElement(cnst.OpEncryptPending))
		})

		It("encrypts after the partitions exist and OEM is mounted", func() {
			// The policy is read from the mounted /run/cos/oem, and
			// ensure-partitions may have created the partition it is about
			// to encrypt on this very boot.
			Expect(inRAMSteps()[cnst.OpEncryptPending].Dependencies).To(ContainElements(
				cnst.OpEnsurePartitions, cnst.OpKcryptUpgrade, cnst.OpMountOEM))
		})
	})

	Context("on a node whose OEM is encrypted", func() {
		BeforeEach(func() { mockOEM(sdkConstants.LUKSFs) })

		It("does not register the encrypt-pending step", func() {
			// Same asymmetry as normal boot: the policy lives on an OEM
			// this branch cannot read until the unlock has run, which is
			// the step encrypt-pending would have to precede. Those nodes
			// were encrypted at install time, so nothing is pending.
			Expect(inRAMSteps()).NotTo(HaveKey(cnst.OpEncryptPending))
		})
	})
})
