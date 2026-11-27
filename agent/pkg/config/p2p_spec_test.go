package config

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kairos-io/kairos/v4/agent/pkg/p2p"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/constants"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkBundles "github.com/kairos-io/kairos/v4/sdk/types/bundles"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/types/install"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// The p2p source is wired into install, upgrade, reset and the trusted boot
// variants the same way the registry options are: before the image is sized.
var _ = Describe("p2p source in the specs (EXPERIMENTAL)", Label("p2p"), func() {
	var seedPort int
	var shas map[string]string
	var cfg *sdkConfig.Config
	var fs *vfst.TestFS
	const mib = 2

	BeforeEach(func() {
		GinkgoT().Setenv("KAIROS_P2P_EXPERIMENTAL", "")
		dir := GinkgoT().TempDir()
		write := func(name string) string {
			b := make([]byte, mib<<20)
			_, err := rand.Read(b)
			Expect(err).ToNot(HaveOccurred())
			p := filepath.Join(dir, name)
			Expect(os.WriteFile(p, b, 0o600)).To(Succeed())
			return p
		}
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		seedPort = l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
		paths := map[string]string{"active": write("active.img"), "passive": write("passive.img"), "recovery": write("recovery.img")}
		shas = map[string]string{}
		for k, v := range paths {
			shas[k], _ = p2p.SHA256File(v)
		}
		allow, _ := p2p.ParseAllowlist([]string{"127.0.0.0/8"})
		s, err := p2p.StartService(context.Background(), p2p.ServiceConfig{
			NetConfig:    p2p.NetConfig{ListenHost: "127.0.0.1", ListenPort: seedPort, Allow: allow},
			Paths:        paths,
			PollInterval: time.Hour,
		})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(s.Close)

		var cleanup func()
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(cleanup)
		devices := &ghwMock.GhwMock{}
		devices.AddDisk(partitions.Disk{Name: "device", Partitions: []*partitions.Partition{
			{Name: "device1", FilesystemLabel: constants.EfiLabel, FS: "vfat", MountPoint: GinkgoT().TempDir()},
			{Name: "device2", FilesystemLabel: constants.StateLabel, FS: "ext4"},
			{Name: "device3", FilesystemLabel: constants.RecoveryLabel, FS: "ext4"},
		}})
		devices.CreateDevices()
		DeferCleanup(devices.Clean)

		runner := v1mock.NewFakeRunner()
		runner.SideEffect = func(cmd string, args ...string) ([]byte, error) {
			if cmd == "cat" {
				return []byte(constants.SystemLabel), nil // reset: "booted from the recovery system"
			}
			return []byte{}, nil
		}
		cfg = NewConfig(WithFs(fs), WithLogger(logger.NewNullLogger()), WithRunner(runner),
			WithMounter(v1mock.NewErrorMounter()), WithSyscall(&v1mock.FakeSyscall{}),
			WithCloudInitRunner(&v1mock.FakeCloudInitRunner{}), WithClient(&v1mock.FakeHTTPClient{}), WithPlatform("linux/amd64"))
		cfg.Install = &install.Install{}
		cfg.Bundles = sdkBundles.Bundles{}
	})

	collect := func(body string) {
		opts := &collector.Options{}
		Expect(opts.Apply(collector.Readers(strings.NewReader(body)), collector.NoLogs)).To(Succeed())
		collected, err := collector.Scan(opts, FilterKeys)
		Expect(err).ToNot(HaveOccurred())
		cfg.Collector = *collected
	}
	block := "p2p-source:\n  experimental: true\n  bind: 127.0.0.2\n  allow: [127.0.0.0/8]\n"
	// every source carries the pin of its image: nothing else would authenticate it
	src := func(image string) string {
		return fmt.Sprintf("p2p://127.0.0.1:%d/%s?sha256=%s", seedPort, image, shas[image])
	}

	It("installs from a p2p source, sizing it from the manifest", func() {
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n", block, src("active")))
		spec, err := NewInstallSpec(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Active.Source.IsP2P()).To(BeTrue())
		Expect(spec.Active.Source.IsRawImage()).To(BeTrue())
		Expect(spec.Active.Size).To(BeEquivalentTo(mib + 100))
		Expect(cfg.P2PSource).ToNot(BeNil())
		Expect(cfg.P2PSource.Bind).To(Equal("127.0.0.2"))
	})

	It("installs the recovery system from the recovery image", func() {
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n  recovery-system:\n    source: %s\n", block, src("active"), src("recovery")))
		spec, err := NewInstallSpec(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Recovery.Source.IsP2P()).To(BeTrue())
	})

	It("refuses the recovery image for the system and the other way round", func() {
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n", block, src("recovery")))
		_, err := NewInstallSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("cannot be deployed as the active or passive")))
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n  recovery-system:\n    source: %s\n", block, src("active"), src("active")))
		_, err = NewInstallSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("p2p://.../recovery")))
	})

	It("refuses to install, upgrade or reset from a p2p source nothing authenticates", func() {
		bare := fmt.Sprintf("p2p://127.0.0.1:%d/active", seedPort)
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n", block, bare))
		_, err := NewInstallSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("refusing p2p source")))
		collect(fmt.Sprintf("#cloud-config\n%supgrade:\n  system:\n    source: %s\n", block, bare))
		_, err = NewUpgradeSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("refusing p2p source")))
		collect(fmt.Sprintf("#cloud-config\n%sreset:\n  system:\n    source: %s\n", block, bare))
		_, err = NewResetSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("refusing p2p source")))
		// the explicit opt in lets it through
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n", block+"  insecure: true\n", bare))
		_, err = NewInstallSpec(cfg)
		Expect(err).ToNot(HaveOccurred())
	})

	It("refuses to install from a p2p source without the experimental opt in", func() {
		collect(fmt.Sprintf("#cloud-config\ninstall:\n  system:\n    source: %s\n", src("active")))
		_, err := NewInstallSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("EXPERIMENTAL")))
	})

	It("fails the install when nobody serves the image, instead of guessing a size", func() {
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: p2p://127.0.0.1:1/active?sha256=%s\n", block, strings.Repeat("ab", 32)))
		_, err := NewInstallSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("could not read the size")))
	})

	It("upgrades from a p2p source", func() {
		collect(fmt.Sprintf("#cloud-config\n%supgrade:\n  system:\n    source: %s\n", block, src("passive")))
		spec, err := NewUpgradeSpec(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Active.Source.IsP2P()).To(BeTrue())
		// the default image size (3 GiB) is larger than the measured one and wins
		Expect(spec.Active.Size).To(BeNumerically(">=", mib+100))
	})

	It("upgrades the recovery system from the recovery image only", func() {
		collect(fmt.Sprintf("#cloud-config\n%supgrade:\n  recovery: true\n  recovery-system:\n    source: %s\n", block, src("recovery")))
		spec, err := NewUpgradeSpec(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Recovery.Source.IsP2P()).To(BeTrue())
		collect(fmt.Sprintf("#cloud-config\n%supgrade:\n  recovery: true\n  recovery-system:\n    source: %s\n", block, src("active")))
		_, err = NewUpgradeSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("p2p://.../recovery")))
	})

	It("resets from a p2p source, sizing both images from it", func() {
		collect(fmt.Sprintf("#cloud-config\n%sreset:\n  system:\n    source: %s\n", block, src("active")))
		spec, err := NewResetSpec(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(spec.Active.Source.IsP2P()).To(BeTrue())
		Expect(spec.Active.Size).To(BeEquivalentTo(mib + 100))
		Expect(spec.Passive.Size).To(BeEquivalentTo(mib + 100))
	})

	It("refuses a p2p source on trusted boot", func() {
		collect(fmt.Sprintf("#cloud-config\n%sinstall:\n  system:\n    source: %s\n", block, src("active")))
		_, err := NewUkiInstallSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("trusted boot")))
		collect(fmt.Sprintf("#cloud-config\n%supgrade:\n  system:\n    source: %s\n", block, src("active")))
		_, err = NewUkiUpgradeSpec(cfg)
		Expect(err).To(MatchError(ContainSubstring("trusted boot")))
	})

})
