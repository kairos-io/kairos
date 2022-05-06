package role

import (
	"errors"
	"fmt"
	"strings"

	"github.com/c3os-io/c3os/cli/config"
	"github.com/c3os-io/c3os/cli/machine"
	"github.com/c3os-io/c3os/cli/utils"

	service "github.com/mudler/edgevpn/api/client/service"
)

func Worker(cc *config.Config) Role {
	return func(c *service.RoleConfig) error {

		if cc.C3OS.Role != "" {
			// propagate role if we were forced by configuration
			// This unblocks eventual auto instances to try to assign roles
			c.Client.Set("role", c.UUID, cc.C3OS.Role)
		}

		if SentinelExist() {
			c.Logger.Info("Node already configured, backing off")
			return nil
		}

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

		ip := utils.GetInterfaceIP("edgevpn0")
		if ip == "" {
			return errors.New("node doesn't have an ip yet")
		}

		c.Logger.Info("Configuring k3s-agent", ip, masterIP, nodeToken)

		svc, err := machine.K3sAgent()
		if err != nil {
			return err
		}

		k3sConfig := config.K3s{}
		if cc.K3sAgent.Enabled {
			k3sConfig = cc.K3sAgent
		}

		env := map[string]string{
			"K3S_URL":   fmt.Sprintf("https://%s:6443", masterIP),
			"K3S_TOKEN": nodeToken,
		}

		if !k3sConfig.ReplaceEnv {
			// Override opts with user-supplied
			for k, v := range k3sConfig.Env {
				env[k] = v
			}
		} else {
			env = k3sConfig.Env
		}

		// Setup systemd unit and starts it
		if err := utils.WriteEnv("/etc/sysconfig/k3s-agent",
			env,
		); err != nil {
			return err
		}

		if err := svc.SetEnvFile("/etc/sysconfig/k3s-agent"); err != nil {
			return err
		}

		args := []string{
			"--with-node-id",
			fmt.Sprintf("--node-ip %s", ip),
			"--flannel-iface=edgevpn0",
		}
		if k3sConfig.ReplaceArgs {
			args = k3sConfig.Args
		} else {
			args = append(args, k3sConfig.Args...)
		}

		if err := svc.OverrideCmd(fmt.Sprintf("/usr/bin/k3s agent %s", strings.Join(args, " "))); err != nil {
			return err
		}

		if err := svc.Start(); err != nil {
			return err
		}

		if err := svc.Enable(); err != nil {
			return err
		}

		CreateSentinel()

		return nil
	}
}
