package role

import (
	"errors"
	"fmt"
	"strings"
	"time"

	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	"github.com/kairos-io/provider-kairos/v2/internal/role"

	service "github.com/mudler/edgevpn/api/client/service"
)

func propagateMasterData(role string, k K8sNode) error {
	c := k.RoleConfig()
	defer func() {
		// Avoid polluting the API.
		// The ledger already retries in the background to update the blockchain, but it has
		// a default timeout where it would stop trying afterwards.
		// Each request here would have it's own background announce, so that can become expensive
		// when network is having lot of changes on its way.
		time.Sleep(30 * time.Second)
	}()

	// If we are configured as master, always signal our role
	if err := c.Client.Set("role", c.UUID, role); err != nil {
		c.Logger.Error(err)
		return err
	}

	if k.HA() && !k.ClusterInit() {
		return nil
	}

	err := k.PropagateData()
	if err != nil {
		c.Logger.Error(err)
	}

	err = c.Client.Set("master", "ip", k.IP())
	if err != nil {
		c.Logger.Error(err)
	}
	return nil
}

// we either return the ElasticIP or the IP from the edgevpn interface.
func guessIP(pconfig *providerConfig.Config) string {
	if pconfig.KubeVIP.EIP != "" {
		return pconfig.KubeVIP.EIP
	}
	return utils.GetInterfaceIP("edgevpn0")
}

func waitForMasterHAInfo(m K8sNode) bool {
	var nodeToken string

	nodeToken, _ = m.Token()
	c := m.RoleConfig()

	if nodeToken == "" {
		c.Logger.Info("the nodetoken is not there yet..")
		return true
	}
	clusterInitIP, _ := c.Client.Get("master", "ip")
	if clusterInitIP == "" {
		c.Logger.Info("the clusterInitIP is not there yet..")
		return true
	}

	return false
}

func Master(cc *sdkConfig.Config, pconfig *providerConfig.Config, roleName string) role.Role { //nolint:revive
	return func(c *service.RoleConfig) error {
		c.Logger.Info(fmt.Sprintf("Starting Master(%s)", roleName))

		ip := guessIP(pconfig)
		// If we don't have an IP, we sit and wait
		if ip == "" {
			return errors.New("node doesn't have an ip yet")
		}
		if err := c.Client.Set("ip", c.UUID, ip); err != nil {
			c.Logger.Error(err)
		}

		c.Logger.Info("Checking role assignment")

		if pconfig.P2P.Role != "" {
			c.Logger.Info(fmt.Sprintf("Setting role from configuration: %s", pconfig.P2P.Role))
			// propagate role if we were forced by configuration
			// This unblocks eventual auto instances to try to assign roles
			if err := c.Client.Set("role", c.UUID, pconfig.P2P.Role); err != nil {
				c.Logger.Error(err)
			}
		}

		c.Logger.Info("Determining K8s distro")
		node, err := NewK8sNode(pconfig)
		if err != nil {
			return fmt.Errorf("stopping Master: %s", err.Error())
		}

		node.SetRole(roleName)
		node.SetRoleConfig(c)
		node.SetIP(ip)
		node.GuessInterface()

		c.Logger.Info("Verifying sentinel file")
		if role.SentinelExist() {
			c.Logger.Info("Node already configured, propagating master data and backing off")
			return propagateMasterData(roleName, node)
		}

		c.Logger.Info("Checking HA")
		if node.HA() && !node.ClusterInit() && waitForMasterHAInfo(node) {
			return nil
		}

		c.Logger.Info("Generating env")
		env := node.GenerateEnv()

		// Configure k8s service to start on edgevpn0
		c.Logger.Info(fmt.Sprintf("Configuring %s", node.Distro()))

		c.Logger.Info("Running bootstrap before stage")
		utils.SH(fmt.Sprintf("kairos-agent run-stage provider-kairos.bootstrap.before.%s", roleName)) //nolint:errcheck

		svc, err := node.Service()
		if err != nil {
			return fmt.Errorf("failed to get %s service: %w", node.Distro(), err)
		}

		c.Logger.Info("Writing service Env %s")
		envUnit := node.EnvUnit()
		if err := utils.WriteEnv(envUnit,
			env,
		); err != nil {
			return fmt.Errorf("failed to write the %s service: %w", node.Distro(), err)
		}

		c.Logger.Info("Generating args")
		args, err := node.GenArgs()
		if err != nil {
			return fmt.Errorf("failed to generate %s args: %w", node.Distro(), err)
		}

		if node.ProviderConfig().KubeVIP.IsEnabled() {
			c.Logger.Info("Configuring KubeVIP")
			if err := node.DeployKubeVIP(); err != nil {
				return fmt.Errorf("failed KubeVIP setup: %w", err)
			}
		}

		k8sBin := node.K8sBin()
		if k8sBin == "" {
			return fmt.Errorf("no %s binary found (?)", node.Distro())
		}

		c.Logger.Info("Writing service override")
		if err := svc.OverrideCmd(fmt.Sprintf("%s %s %s", k8sBin, node.Role(), strings.Join(args, " "))); err != nil {
			return fmt.Errorf("failed to override %s command: %w", node.Distro(), err)
		}

		c.Logger.Info("Starting service")
		if err := svc.Start(); err != nil {
			return fmt.Errorf("failed to start %s service: %w", node.Distro(), err)
		}

		c.Logger.Info("Enabling service")
		if err := svc.Enable(); err != nil {
			return fmt.Errorf("failed to enable %s service: %w", node.Distro(), err)
		}

		c.Logger.Info("Propagating master data")
		if err := propagateMasterData(roleName, node); err != nil {
			return fmt.Errorf("failed to propagate master data: %w", err)
		}

		c.Logger.Info("Running after bootstrap stage")
		utils.SH(fmt.Sprintf("kairos-agent run-stage provider-kairos.bootstrap.after.%s", roleName)) //nolint:errcheck

		c.Logger.Info("Creating sentinel")
		if err := role.CreateSentinel(); err != nil {
			return fmt.Errorf("failed to create sentinel: %w", err)
		}

		return nil
	}
}
