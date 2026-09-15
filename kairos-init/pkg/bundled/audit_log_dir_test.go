package bundled_test

import (
	"io"
	"os"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/mudler/yip/pkg/plugins"
	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/twpayne/go-vfs/v5/vfst"
	"gopkg.in/yaml.v3"
)

const auditLogDir = "/var/log/audit"

// rootfsConfig parses the shipped 00_rootfs.yaml with the same unmarshaller yip
// uses (loader_yip.go, gopkg.in/yaml.v3 into schema.YipConfig), so the specs see
// the permissions the running system sees and not a hand-rolled reading of them.
func rootfsConfig() *schema.YipConfig {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/00_rootfs.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config schema.YipConfig
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())
	return &config
}

// auditDirEntry returns the /var/log/audit directory declaration and the stage
// it belongs to, searching every stage of the file so a spec can assert which
// stage that is rather than assuming it.
func auditDirEntry() (string, schema.Directory) {
	for stageName, stages := range rootfsConfig().Stages {
		for _, stage := range stages {
			for _, dir := range stage.Directories {
				if dir.Path == auditLogDir {
					return stageName, dir
				}
			}
		}
	}

	Fail("no stage of 00_rootfs.yaml creates " + auditLogDir)
	return "", schema.Directory{}
}

// ensureAuditDir runs yip's own directories plugin over the shipped
// declaration and returns the resulting mode. Owner and group are swapped for
// the current user's: the declared root:root is asserted separately, and
// chowning to root fails for an unprivileged suite, which would mask the mode.
func ensureAuditDir(existing map[string]interface{}) os.FileMode {
	_, dir := auditDirEntry()
	dir.Owner = os.Getuid()
	dir.Group = os.Getgid()

	fs, cleanup, err := vfst.NewTestFS(existing)
	Expect(err).ToNot(HaveOccurred())
	defer cleanup()

	log := logrus.New()
	log.SetOutput(io.Discard)

	Expect(plugins.EnsureDirectories(log, schema.Stage{
		Directories: []schema.Directory{dir},
	}, fs, nil)).To(Succeed())

	info, err := fs.Stat(auditLogDir)
	Expect(err).ToNot(HaveOccurred())
	return info.Mode().Perm()
}

var _ = Describe("AuditLogDir", func() {
	It("declares the directory root-owned and 0700", func() {
		_, dir := auditDirEntry()

		// Reads 448, not 700: the unquoted mode in the YAML has to survive as
		// octal, or auditd's directory ends up with a mode nobody asked for.
		Expect(dir.Permissions).To(Equal(uint32(0700)))
		Expect(dir.Owner).To(Equal(0))
		Expect(dir.Group).To(Equal(0))
	})

	// immucore runs the initramfs stage after the persistent bind mounts
	// (dag_normal_boot.go wires OpInitramfsHook after OpMountBind), so the stage
	// has to be initramfs for the mode to land on the persistent directory
	// instead of on the ephemeral one it is about to be shadowed by.
	It("creates the directory in the initramfs stage", func() {
		stage, _ := auditDirEntry()
		Expect(stage).To(Equal("initramfs"))
	})

	// auditd runs under OpenRC too, so this must not be gated the way the
	// sibling /var/log/journal step is.
	It("creates the directory under every service manager", func() {
		for _, stage := range rootfsConfig().Stages["initramfs"] {
			for _, dir := range stage.Directories {
				if dir.Path == auditLogDir {
					Expect(stage.OnlyIfServiceManager).To(BeEmpty())
					Expect(stage.If).To(BeEmpty())
				}
			}
		}
	})

	It("creates a missing directory 0700", func() {
		Expect(ensureAuditDir(map[string]interface{}{
			"/var/log": &vfst.Dir{Perm: 0o755},
		})).To(Equal(os.FileMode(0700)))
	})

	// The persistent partition can hand back a directory an older install left
	// world-readable. Re-running on every boot is only worth anything if it
	// tightens that one instead of accepting it.
	It("tightens a directory left loose by an older install", func() {
		Expect(ensureAuditDir(map[string]interface{}{
			auditLogDir: &vfst.Dir{Perm: 0o755},
		})).To(Equal(os.FileMode(0700)))
	})

	It("leaves an already correct directory alone", func() {
		Expect(ensureAuditDir(map[string]interface{}{
			auditLogDir: &vfst.Dir{Perm: 0o700},
		})).To(Equal(os.FileMode(0700)))
	})

	// /var/log is persisted already, so the audit trail rides along. Listing the
	// subdirectory as well would rsync and bind-mount a path nested inside a
	// path immucore is already rsyncing and bind-mounting.
	It("is not listed separately in the persistent state paths", func() {
		Expect(persistentStatePaths()).To(ContainElement("/var/log"))
		Expect(persistentStatePaths()).ToNot(ContainElement(auditLogDir))
	})
})
