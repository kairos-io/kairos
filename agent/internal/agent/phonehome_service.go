package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	"github.com/kairos-io/kairos-sdk/constants"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
)

func phoneHomeServiceUnit(agentBin string) string {
	return fmt.Sprintf(`[Unit]
Description=Kairos Agent Phone Home
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s phone-home
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, agentBin)
}

// enablePhoneHomeIfConfigured installs and starts the phone-home systemd service
// when a `phonehome:` section with a url is present in the merged cloud-config.
// The config is parsed by phonehome.LoadFromCollector so it uses the same
// Collector output as the rest of the kairos-agent (no separate file walk).
func enablePhoneHomeIfConfigured(c *sdkConfig.Config) {
	// Point the phonehome package at the agent's already-configured logger
	// so the service-install output below and the command handlers' output
	// later share one stream (journald when the binary is running under
	// systemd, /var/log/kairos/*.log + stderr otherwise).
	phonehome.SetLogger(c.Logger)

	cfg, ok, err := phonehome.LoadFromCollector(c)
	if err != nil {
		c.Logger.Warnf("could not parse phonehome config: %v", err)
		return
	}
	if !ok || cfg.URL == "" {
		return
	}

	c.Logger.Infof("phone-home configuration detected, enabling phone-home service")

	if err := os.WriteFile(phonehome.ServicePath, []byte(phoneHomeServiceUnit(constants.AgentDefaultPath)), 0600); err != nil {
		c.Logger.Warnf("failed to write phone-home service: %v", err)
		return
	}

	for _, args := range [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", phonehome.ServiceName},
		{"systemctl", "start", phonehome.ServiceName},
	} {
		// args is a literal slice defined just above — no user input reaches exec.Command.
		cmd := exec.Command(args[0], args[1:]...) //nosec G204 -- fixed command and arguments
		if out, err := cmd.CombinedOutput(); err != nil {
			c.Logger.Warnf("%s failed: %v (%s)", strings.Join(args, " "), err, string(out))
		}
	}
}
