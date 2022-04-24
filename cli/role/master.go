package role

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/c3os-io/c3os/cli/config"
	"github.com/c3os-io/c3os/cli/machine"
	"github.com/c3os-io/c3os/cli/utils"

	service "github.com/mudler/edgevpn/api/client/service"
)

func propagateMasterData(ip string, c *service.RoleConfig) error {
	defer func() {
		// Avoid polluting the API.
		// The ledger already retries in the background to update the blockchain, but it has
		// a default timeout where it would stop trying afterwards.
		// Each request here would have it's own background announce, so that can become expensive
		// when network is having lot of changes on its way.
		time.Sleep(30 * time.Second)
	}()

	// If we are configured as master, always signal our role
	c.Client.Set("role", c.UUID, "master")

	tokenB, err := ioutil.ReadFile("/var/lib/rancher/k3s/server/node-token")
	if err != nil {
		c.Logger.Error(err)
		return err
	}

	nodeToken := string(tokenB)
	nodeToken = strings.TrimRight(nodeToken, "\n")
	if nodeToken != "" {
		c.Client.Set("nodetoken", "token", nodeToken)
	}

	kubeB, err := ioutil.ReadFile("/etc/rancher/k3s/k3s.yaml")
	if err != nil {
		c.Logger.Error(err)
		return err
	}
	kubeconfig := string(kubeB)
	if kubeconfig != "" {
		c.Client.Set("kubeconfig", "master", base64.RawURLEncoding.EncodeToString(kubeB))
	}
	c.Client.Set("master", "ip", ip)
	return nil
}

func Master(cc *config.Config) Role {
	return func(c *service.RoleConfig) error {

		ip := utils.GetInterfaceIP("edgevpn0")
		if ip == "" {
			return errors.New("node doesn't have an ip yet")
		}

		if cc.C3OS.Role != "" {
			// propagate role if we were forced by configuration
			// This unblocks eventual auto instances to try to assign roles
			c.Client.Set("role", c.UUID, cc.C3OS.Role)
		}

		if SentinelExist() {
			c.Logger.Info("Node already configured, backing off")
			return propagateMasterData(ip, c)
		}

		// Configure k3s service to start on edgevpn0
		c.Logger.Info("Configuring k3s")

		svc, err := machine.K3s()
		if err != nil {
			return err
		}

		env := map[string]string{}
		if !cc.K3s.ReplaceEnv {
			// Override opts with user-supplied
			for k, v := range cc.K3s.Env {
				env[k] = v
			}
		} else {
			env = cc.K3s.Env
		}

		// Setup systemd unit and starts it
		if err := utils.WriteEnv("/etc/sysconfig/k3s",
			env,
		); err != nil {
			return err
		}

		args := []string{"--flannel-iface=edgevpn0"}
		if cc.K3s.ReplaceArgs {
			args = cc.K3s.Args
		} else {
			args = append(args, cc.K3s.Args...)
		}

		if err := svc.OverrideCmd(fmt.Sprintf("/usr/bin/k3s server %s", strings.Join(args, " "))); err != nil {
			return err
		}

		if err := svc.SetEnvFile("/etc/sysconfig/k3s"); err != nil {
			return err
		}

		if err := svc.Start(); err != nil {
			return err
		}

		if err := svc.Enable(); err != nil {
			return err
		}

		propagateMasterData(ip, c)

		CreateSentinel()

		return nil
	}
}

// TODO: https://rancher.com/docs/k3s/latest/en/installation/ha-embedded/
func HAMaster(c *service.RoleConfig) {
	c.Logger.Info("HA Role not implemented yet")
}
