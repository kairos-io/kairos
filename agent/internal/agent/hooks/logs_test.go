package hook_test

import (
	"bytes"

	hook "github.com/kairos-io/kairos/v4/agent/internal/agent/hooks"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	cnst "github.com/kairos-io/kairos/v4/agent/pkg/constants"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// byLabelPersistent is the path this hook used to hand to mount(2). It is
// ambiguous on an encrypted install, where the raw crypto_LUKS partition and
// its unlocked mapper both advertise COS_PERSISTENT, so udev's last writer
// decides where it points. See kairos-io/kairos#4685.
const byLabelPersistent = "/dev/disk/by-label/" + cnst.PersistentLabel

var _ = Describe("CopyLogs", func() {
	var cfg *sdkConfig.Config
	var fs vfs.FS
	var runner *v1mock.FakeRunner
	var syscallMock *v1mock.FakeSyscall
	var ghwTest ghwMock.GhwMock
	var memLog *bytes.Buffer
	var cleanup func()
	var err error

	// mountsOf returns every source CopyLogs handed to mount(2) for the
	// persistent mountpoint. FakeSyscall only exposes an exact-match helper,
	// so the specs below assert on the pair (wanted source present, by-label
	// source absent) rather than on a recorded list.
	mountedFrom := func(source string) bool {
		return syscallMock.WasMountCalledWith(source, cnst.PersistentDir, "ext4", 0, "")
	}

	// withPersistent installs a ghw view of the disk and runs the hook.
	run := func(parts ...*sdkPartitions.Partition) error {
		ghwTest = ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: parts})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)
		return hook.CopyLogs{}.Run(*cfg, nil)
	}

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		syscallMock = &v1mock.FakeSyscall{}
		memLog = &bytes.Buffer{}
		logger := sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{"/var/log/": &vfst.Dir{Perm: 0755}})
		Expect(err).ShouldNot(HaveOccurred())
		cfg = config.NewConfig(
			config.WithFs(fs),
			config.WithRunner(runner),
			config.WithLogger(logger),
			config.WithSyscall(syscallMock),
		)
		cfg.Collector = collector.Config{}
	})

	AfterEach(func() {
		cleanup()
	})

	It("mounts the LUKS mapper, never the ambiguous by-label symlink", func() {
		// The layout from the bug report: the raw partition is the
		// crypto_LUKS container and carries the label, and the plaintext
		// ext4 only exists behind the mapper. CopyLogs runs straight after
		// Encrypt, while that mapper is still unlocked.
		Expect(run(
			&sdkPartitions.Partition{
				Name:            "vda5",
				PartitionLabel:  "persistent",
				FilesystemLabel: cnst.PersistentLabel,
				FS:              "crypto_LUKS",
			},
		)).Should(Succeed())

		Expect(mountedFrom("/dev/mapper/vda5")).To(BeTrue(),
			"CopyLogs must mount the unlocked mapper, got mounts: %s", memLog.String())
		Expect(mountedFrom(byLabelPersistent)).To(BeFalse(),
			"CopyLogs must not hand the ambiguous by-label path to mount(2)")
	})

	It("mounts the plain partition when persistent is not encrypted", func() {
		Expect(run(
			&sdkPartitions.Partition{
				Name:            "vda5",
				Path:            "/dev/vda5",
				PartitionLabel:  "persistent",
				FilesystemLabel: cnst.PersistentLabel,
				FS:              "ext4",
			},
		)).Should(Succeed())

		Expect(mountedFrom("/dev/vda5")).To(BeTrue(),
			"CopyLogs must mount the partition ghw reported, got: %s", memLog.String())
		Expect(mountedFrom(byLabelPersistent)).To(BeFalse(),
			"CopyLogs must not hand the ambiguous by-label path to mount(2)")
	})

	It("warns and stays best-effort when the label resolves to nothing", func() {
		// No COS_PERSISTENT anywhere. The hook is best effort: losing the
		// install logs must not take the install down with it.
		Expect(run(
			&sdkPartitions.Partition{
				Name:            "vda1",
				Path:            "/dev/vda1",
				FilesystemLabel: cnst.OEMLabel,
				FS:              "ext4",
			},
		)).Should(Succeed())

		Expect(mountedFrom(byLabelPersistent)).To(BeFalse())
		Expect(memLog.String()).To(ContainSubstring("could not mount persistent"))
	})
})
