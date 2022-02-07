package role

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mudler/c3os/installer/systemd"
	service "github.com/mudler/edgevpn/api/client/service"
)

func Worker(c *service.RoleConfig) error {

	masterIP, _ := c.Client.Get("master", "ip")
	if masterIP == "" {
		c.Logger.Info("MasterIP not there still..")
		return nil
	}

	nodeToken, _ := c.Client.Get("nodetoken", "token")
	if masterIP == "" {
		c.Logger.Info("nodetoken not there still..")
		return nil
	}

	nodeToken = strings.TrimRight(nodeToken, "\n")

	// K3S_URL=https://10.1.0.3:6443
	// K3S_TOKEN=xx
	// k3s agent --flannel-iface=edgevpn0 --node-ip 10.1.0.4

	ip := getIP()
	if ip == "" {
		return errors.New("node doesn't have an ip yet")
	}

	c.Logger.Info("Configuring k3s-agent", ip, masterIP, nodeToken)

	// Setup systemd unit and starts it
	if err := systemd.WriteEnv("/etc/systemd/system/k3s-agent.service.env",
		map[string]string{
			"K3S_URL":   fmt.Sprintf("https://%s:6443", masterIP),
			"K3S_TOKEN": nodeToken,
		},
	); err != nil {
		return err
	}

	if err := systemd.OverrideServiceCmd("k3s-agent", fmt.Sprintf("/usr/bin/k3s agent --with-node-id --node-ip %s --flannel-iface=edgevpn0", ip)); err != nil {
		return err
	}

	if err := systemd.StartUnitBlocking("k3s-agent"); err != nil {
		return err
	}

	if err := systemd.EnableUnit("k3s-agent"); err != nil {
		return err
	}

	CreateSentinel()

	os.Exit(0)
	return nil
}
