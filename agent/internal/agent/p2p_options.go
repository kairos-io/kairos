package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkImages "github.com/kairos-io/kairos/v4/sdk/types/images"
)

// P2PServiceName / P2PServicePath describe the opt-in seeding unit.
const (
	P2PServiceName = "kairos-agent-p2p"
	P2PServicePath = "/etc/systemd/system/kairos-agent-p2p.service"
)

// LoadP2POptionsFromDirs scans the given cloud-config dirs for the p2p-source block.
func LoadP2POptionsFromDirs(dirs []string) (sdkImages.P2POptions, error) {
	c, err := config.ScanNoLogs(collector.Directories(dirs...))
	if err != nil {
		return sdkImages.P2POptions{}, err
	}
	return config.P2POptionsFromConfig(c)
}

// P2PCacheDir is where the seeder keeps its computed manifests, the only
// place the hardened unit may write to.
const P2PCacheDir = "/usr/local/.kairos/p2p-cache"

// p2pServiceUnit is the seeding unit. The seeder only reads the images and the
// cloud-config and writes its manifest cache: it gets a sandbox to match, so a
// bug in the HTTP path is not root on the node.
func p2pServiceUnit(agentBin string) string {
	return fmt.Sprintf(`[Unit]
Description=Kairos Agent p2p image seeder (EXPERIMENTAL)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s p2p seed
Restart=on-failure
RestartSec=10
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
UMask=0077
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
# AF_NETLINK: the seeder lists the network interfaces
RestrictAddressFamilies=AF_INET AF_UNIX AF_NETLINK
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallFilter=~@privileged
CapabilityBoundingSet=
AmbientCapabilities=
ReadOnlyPaths=-/run/initramfs -/oem -/usr/local/cloud-config -/etc/kairos
ReadWritePaths=%s
LimitNOFILE=4096
TasksMax=256
MemoryMax=512M

[Install]
WantedBy=multi-user.target
`, agentBin, P2PCacheDir)
}

// enableP2PIfConfigured installs and starts the experimental seeding unit only
// when the cloud-config opts in with `p2p-source.enabled: true` and
// `p2p-source.experimental: true`. Off by default.
func enableP2PIfConfigured(c *sdkConfig.Config) {
	opts, err := config.P2POptionsFromConfig(c)
	if err != nil {
		c.Logger.Warnf("could not parse the p2p-source config: %v", err)
		return
	}
	if !opts.Enabled {
		return
	}
	if !opts.Experimental {
		c.Logger.Warnf("p2p-source.enabled is set but p2p-source.experimental is not: the seeding service is EXPERIMENTAL and stays off until you set p2p-source.experimental: true")
		return
	}
	c.Logger.Warnf("starting the EXPERIMENTAL p2p seeding service")
	// the unit may only write here, and systemd needs the path to exist
	if err := os.MkdirAll(P2PCacheDir, 0o700); err != nil {
		c.Logger.Warnf("failed to create %s: %v", P2PCacheDir, err)
		return
	}
	if err := os.WriteFile(P2PServicePath, []byte(p2pServiceUnit(sdkConstants.AgentDefaultPath)), 0600); err != nil {
		c.Logger.Warnf("failed to write p2p service: %v", err)
		return
	}
	for _, args := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", P2PServiceName},
		{"systemctl", "start", P2PServiceName},
	} {
		// args is a literal slice defined just above, no user input reaches exec.Command.
		cmd := exec.Command(args[0], args[1:]...) //nosec G204 -- fixed command and arguments
		if out, err := cmd.CombinedOutput(); err != nil {
			c.Logger.Warnf("%s failed: %v (%s)", strings.Join(args, " "), err, string(out))
		}
	}
}

// DefaultSeedDirs returns the state and recovery dirs the seeder reads from.
func DefaultSeedDirs() (state, recovery string) {
	return constants.RunningStateDir, constants.RunningRecoveryStateDir
}
