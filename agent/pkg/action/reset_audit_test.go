package action_test

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/action"
	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("The audit trail across a reset", Label("reset"), func() {
	var config *sdkConfig.Config
	var fs vfs.FS
	var cleanup func()
	var persistent *sdkPartitions.Partition
	var memLog *bytes.Buffer

	// auditDir is the backing directory of the /var/log/audit bind on the
	// persistent partition, as immucore lays it out.
	auditDir := func() string {
		return filepath.Join(constants.PersistentDir, constants.AuditLogStatePath)
	}

	BeforeEach(func() {
		var err error
		fs, cleanup, err = vfst.NewTestFS(map[string]interface{}{})
		Expect(err).ShouldNot(HaveOccurred())
		memLog = &bytes.Buffer{}
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(v1mock.NewFakeRunner()),
			agentConfig.WithLogger(sdkLogger.NewBufferLogger(memLog)),
			agentConfig.WithMounter(v1mock.NewErrorMounter()),
			agentConfig.WithSyscall(&v1mock.FakeSyscall{}),
		)
		// Reset runs from the recovery system, which boots without the
		// persistent volume: no mount point, not mounted.
		persistent = &sdkPartitions.Partition{
			Name:            "device5",
			FilesystemLabel: "COS_PERSISTENT",
			FS:              "ext4",
			Path:            "/dev/device5",
		}
	})

	AfterEach(func() { cleanup() })

	// format stands in for FormatPartition: everything that was on the
	// partition is gone.
	format := func() {
		Expect(fs.RemoveAll(constants.PersistentDir)).To(Succeed())
	}

	It("carries the audit trail over the format", func() {
		Expect(fsutils.MkdirAll(fs, filepath.Join(auditDir(), "old"), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "old", "audit.log.1"), []byte("type=DAEMON_END\n"), 0o600)).To(Succeed())

		stash, err := action.StashAuditLog(config, persistent)
		Expect(err).ToNot(HaveOccurred())
		Expect(stash).ToNot(BeEmpty())

		format()
		Expect(action.RestoreAuditLog(config, persistent, stash)).To(Succeed())

		content, err := fs.ReadFile(filepath.Join(auditDir(), "audit.log"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("type=DAEMON_START\n"))
		// A rotated log in a subdirectory comes along too.
		content, err = fs.ReadFile(filepath.Join(auditDir(), "old", "audit.log.1"))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("type=DAEMON_END\n"))
	})

	It("restores the directory root-only", func() {
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		stash, err := action.StashAuditLog(config, persistent)
		Expect(err).ToNot(HaveOccurred())
		format()
		Expect(action.RestoreAuditLog(config, persistent, stash)).To(Succeed())

		info, err := fs.Stat(auditDir())
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(constants.AuditLogDirPerm)))
	})

	It("cleans the staging directory up", func() {
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		stash, err := action.StashAuditLog(config, persistent)
		Expect(err).ToNot(HaveOccurred())
		format()
		Expect(action.RestoreAuditLog(config, persistent, stash)).To(Succeed())

		exists, err := fsutils.Exists(fs, stash)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
	})

	It("has nothing to preserve on a node that never ran auditd", func() {
		stash, err := action.StashAuditLog(config, persistent)

		Expect(err).ToNot(HaveOccurred())
		Expect(stash).To(BeEmpty())
		// And the restore of nothing does not create anything.
		Expect(action.RestoreAuditLog(config, persistent, stash)).To(Succeed())
		exists, err := fsutils.Exists(fs, auditDir())
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
	})

	It("has nothing to preserve when the audit trail is empty", func() {
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())

		stash, err := action.StashAuditLog(config, persistent)

		Expect(err).ToNot(HaveOccurred())
		Expect(stash).To(BeEmpty())
	})

	It("does nothing when there is no persistent partition", func() {
		stash, err := action.StashAuditLog(config, nil)

		Expect(err).ToNot(HaveOccurred())
		Expect(stash).To(BeEmpty())
		Expect(action.RestoreAuditLog(config, nil, "/tmp/whatever")).To(Succeed())
	})

	It("leaves the mount state as it found it", func() {
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		_, err := action.StashAuditLog(config, persistent)

		Expect(err).ToNot(HaveOccurred())
		// Recovery does not mount the persistent volume, so neither does the
		// reset once it is done reading from it.
		Expect(persistent.MountPoint).To(BeEmpty())
		mounts, err := config.Mounter.List()
		Expect(err).ToNot(HaveOccurred())
		for _, mnt := range mounts {
			Expect(mnt.Path).ToNot(Equal(constants.PersistentDir))
		}
	})
})
