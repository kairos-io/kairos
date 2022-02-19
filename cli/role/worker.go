package role

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/c3os-io/c3os/cli/machine"
	"github.com/c3os-io/c3os/cli/utils"

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

	ip := getIP()
	if ip == "" {
		return errors.New("node doesn't have an ip yet")
	}

	c.Logger.Info("Configuring k3s-agent", ip, masterIP, nodeToken)

	svc, err := machine.K3sAgent()
	if err != nil {
		return err
	}

	// Setup systemd unit and starts it
	if err := utils.WriteEnv("/etc/sysconfig/k3s-agent",
		map[string]string{
			"K3S_URL":   fmt.Sprintf("https://%s:6443", masterIP),
			"K3S_TOKEN": nodeToken,
		},
	); err != nil {
		return err
	}

	if err := svc.SetEnvFile("/etc/sysconfig/k3s-agent"); err != nil {
		return err
	}

	if err := svc.OverrideCmd(fmt.Sprintf("/usr/bin/k3s agent --with-node-id --node-ip %s --flannel-iface=edgevpn0", ip)); err != nil {
		return err
	}

	if err := svc.Start(); err != nil {
		return err
	}

	if err := svc.Enable(); err != nil {
		return err
	}

	CreateSentinel()

	os.Exit(0)
	return nil
}
