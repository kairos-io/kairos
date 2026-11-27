package agent_test

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/internal/agent"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("p2p-source cloud-config block (EXPERIMENTAL)", func() {
	load := func(yaml string) (sdkImages.P2POptions, error) {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "10_p2p.yaml"), []byte("#cloud-config\n"+yaml), 0o600)).To(Succeed())
		return agent.LoadP2POptionsFromDirs([]string{dir})
	}
	It("is off by default", func() {
		o, err := load("stages: {}\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(o.Enabled).To(BeFalse())
		Expect(o.Experimental).To(BeFalse())
		Expect(o.DisableAnnounce).To(BeFalse())
	})
	It("reads the limits and the insecure opt in", func() {
		o, err := load("p2p-source:\n  insecure: true\n  max-image-size: 100\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(o.Insecure).To(BeTrue())
		Expect(o.MaxImageSize).To(Equal(100))
	})
	It("reads every tunable", func() {
		o, err := load(`p2p-source:
  enabled: true
  experimental: true
  bind: 10.0.0.2
  port: 4242
  allow: [10.0.0.0/24, 192.168.1.5]
  upload-limit: 2048
  max-peers: 7
  disable-announce: true
`)
		Expect(err).ToNot(HaveOccurred())
		Expect(o.Enabled && o.Experimental && o.DisableAnnounce).To(BeTrue())
		Expect(o.Bind).To(Equal("10.0.0.2"))
		Expect(o.Port).To(Equal(4242))
		Expect(o.Allow).To(Equal([]string{"10.0.0.0/24", "192.168.1.5"}))
		Expect(o.UploadLimit).To(Equal(2048))
		Expect(o.MaxPeers).To(Equal(7))
	})
	It("does not confuse it with the kairos p2p network block", func() {
		o, err := load("p2p:\n  network_token: abc\n  auto:\n    enable: true\n")
		Expect(err).ToNot(HaveOccurred())
		Expect(o.Enabled || o.Experimental).To(BeFalse())
	})
	It("refuses a typo", func() {
		_, err := load("p2p-source:\n  experimentl: true\n")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("p2p seeding unit and upgrade flags", func() {
	It("runs the seed command and nothing else", func() {
		u := agent.P2PServiceUnitForTest("/usr/bin/kairos-agent")
		Expect(u).To(ContainSubstring("ExecStart=/usr/bin/kairos-agent p2p seed\n"))
		Expect(u).To(ContainSubstring("EXPERIMENTAL"))
		Expect(strings.Count(u, "ExecStart=")).To(Equal(1))
	})
	// P2P-13: the unit is sandboxed, the seeder only reads images and config and
	// writes its manifest cache
	It("is sandboxed", func() {
		u := agent.P2PServiceUnitForTest("/usr/bin/kairos-agent")
		for _, d := range []string{
			"NoNewPrivileges=yes", "ProtectSystem=strict", "ProtectHome=yes", "PrivateTmp=yes", "PrivateDevices=yes",
			"ProtectKernelTunables=yes", "ProtectKernelModules=yes", "ProtectKernelLogs=yes", "ProtectControlGroups=yes",
			"ProtectClock=yes", "ProtectHostname=yes", "ProtectProc=invisible", "ProcSubset=pid", "UMask=0077", "LockPersonality=yes", "MemoryDenyWriteExecute=yes",
			"RestrictNamespaces=yes", "RestrictRealtime=yes", "RestrictSUIDSGID=yes",
			"RestrictAddressFamilies=AF_INET AF_UNIX AF_NETLINK", "SystemCallArchitectures=native",
			"SystemCallFilter=@system-service", "SystemCallFilter=~@privileged",
			"CapabilityBoundingSet=\n", "AmbientCapabilities=\n", "LimitNOFILE=4096", "TasksMax=256", "MemoryMax=512M",
			"ReadWritePaths=" + agent.P2PCacheDir + "\n", "ReadOnlyPaths=-/run/initramfs -/oem -/usr/local/cloud-config -/etc/kairos",
		} {
			Expect(u).To(ContainSubstring(d))
		}
		Expect(u).ToNot(ContainSubstring("AF_INET6"))
		Expect(u).ToNot(ContainSubstring("ProtectSystem=full"))
	})
	It("puts the upgrade flags into the generated config as the p2p-source block", func() {
		agent.UpgradeP2PFlags = &agent.ExtraConfigP2PSource{Experimental: true, Bind: "10.0.0.9", Allow: []string{"10.0.0.0/24"}, Port: 5000, Insecure: true}
		DeferCleanup(func() { agent.UpgradeP2PFlags = nil })
		out, err := agent.GenerateUpgradeConfForCLIArgsForTest("p2p://10.0.0.2/active")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring(`"p2p-source":{"experimental":true,"bind":"10.0.0.9","port":5000,"allow":["10.0.0.0/24"],"insecure":true}`))
		Expect(out).To(ContainSubstring(`"source":"p2p://10.0.0.2/active"`))
		agent.UpgradeP2PFlags = nil
		out, err = agent.GenerateUpgradeConfForCLIArgsForTest("p2p://10.0.0.2/active")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).ToNot(ContainSubstring("p2p-source"))
	})
})
