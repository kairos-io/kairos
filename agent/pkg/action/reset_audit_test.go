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
	var mounter *v1mock.ErrorMounter

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
		mounter = v1mock.NewErrorMounter()
		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(v1mock.NewFakeRunner()),
			agentConfig.WithLogger(sdkLogger.NewBufferLogger(memLog)),
			agentConfig.WithMounter(mounter),
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

	It("does not carry immucore's migration marker over the format", func() {
		// The marker that records the one-time migration into the bind sits
		// next to the backing directory, so a stash of the directory does not
		// pick it up. It has to stay that way: the marker would say a
		// migration finished on a partition that was just formatted, and the
		// next boot has to be free to sync what the image ships at
		// /var/log/audit into the new backing directory.
		marker := auditDir() + ".migrated"
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(marker, nil, 0o600)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		stash, err := action.StashAuditLog(config, persistent)
		Expect(err).ToNot(HaveOccurred())
		format()
		Expect(action.RestoreAuditLog(config, persistent, stash)).To(Succeed())

		exists, err := fsutils.Exists(fs, marker)
		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
		// And the trail itself still came across.
		Expect(fsutils.Exists(fs, filepath.Join(auditDir(), "audit.log"))).To(BeTrue())
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

	It("stages the trail on /run", func() {
		// /run is a tmpfs on every boot. The default temp dir is not: a
		// recovery boot sets no RW_PATHS, /tmp is not on the list immucore
		// falls back to, and staging there fails on a read-only root.
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())

		stash, err := action.StashAuditLog(config, persistent)

		Expect(err).ToNot(HaveOccurred())
		Expect(stash).To(HavePrefix("/run/"))
	})

	It("reports the failure to unmount the persistent partition", func() {
		Expect(fsutils.MkdirAll(fs, auditDir(), constants.DirPerm)).To(Succeed())
		Expect(fs.WriteFile(filepath.Join(auditDir(), "audit.log"), []byte("type=DAEMON_START\n"), 0o600)).To(Succeed())
		mounter.ErrorOnUnmount = true

		_, err := action.StashAuditLog(config, persistent)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unmounting the persistent partition"))
		// And the mount point stays on the partition, because the device is
		// still mounted: utils.IsMounted and elemental.UnmountPartition both
		// read "not mounted" off an empty mount point, so the unmount the
		// reset does before the format would no-op and mkfs would get a live
		// filesystem.
		Expect(persistent.MountPoint).To(Equal(constants.PersistentDir))
	})

	It("names the path it looked at when there is no trail", func() {
		// PERSISTENT_STATE_TARGET is configurable and a reset cannot read the
		// booted system's cos-layout.env, so "no trail" and "wrong path" look
		// the same from here unless the path is logged.
		_, err := action.StashAuditLog(config, persistent)

		Expect(err).ToNot(HaveOccurred())
		Expect(memLog.String()).To(ContainSubstring(auditDir()))
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
