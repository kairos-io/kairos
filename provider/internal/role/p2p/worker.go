package role

import (
	"fmt"
	"strings"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/utils"

	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	"github.com/kairos-io/provider-kairos/v2/internal/role"
	service "github.com/mudler/edgevpn/api/client/service"
)

func Worker(cc *sdkConfig.Config, pconfig *providerConfig.Config) role.Role { //nolint:revive
	return func(c *service.RoleConfig) error {
		c.Logger.Info("Starting Worker")

		if pconfig.P2P.Role != "" {
			// propagate role if we were forced by configuration
			// This unblocks eventual auto instances to try to assign roles
			if err := c.Client.Set("role", c.UUID, pconfig.P2P.Role); err != nil {
				return err
			}
		}

		if role.SentinelExist() {
			c.Logger.Info("Node already configured, backing off")
			return nil
		}

		masterIP, _ := c.Client.Get("master", "ip")
		if masterIP == "" {
			c.Logger.Info("MasterIP not there still..")
			return nil
		}

		node, err := NewK8sNode(pconfig)
		if err != nil {
			return fmt.Errorf("stopping Worker: %s", err.Error())
		}

		ip := guessIP(pconfig)
		if ip != "" {
			if err := c.Client.Set("ip", c.UUID, ip); err != nil {
				c.Logger.Error(err)
			}
		}

		node.SetRole(RoleWorker)
		node.SetRoleConfig(c)
		node.SetIP(ip)

		nodeToken, _ := node.Token()
		if nodeToken == "" {
			c.Logger.Info("node token not there still..")
			return nil
		}

		utils.SH("kairos-agent run-stage provider-kairos.bootstrap.before.worker") //nolint:errcheck

		err = node.SetupWorker(masterIP, nodeToken)
		if err != nil {
			return err
		}

		k8sBin := node.K8sBin()
		if k8sBin == "" {
			return fmt.Errorf("no %s binary found (?)", node.Distro())
		}

		args, err := node.WorkerArgs()
		if err != nil {
			return err
		}

		svc, err := node.Service()
		if err != nil {
			return err
		}

		c.Logger.Info(fmt.Sprintf("Configuring %s worker", node.Distro()))
		if err := svc.OverrideCmd(fmt.Sprintf("%s %s %s", k8sBin, node.Role(), strings.Join(args, " "))); err != nil {
			return err
		}

		if err := svc.Start(); err != nil {
			return err
		}

		if err := svc.Enable(); err != nil {
			return err
		}

		utils.SH("kairos-agent run-stage provider-kairos.bootstrap.after.worker") //nolint:errcheck

		return role.CreateSentinel()
	}
}
