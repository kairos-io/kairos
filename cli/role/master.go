package role

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/c3os-io/c3os/cli/config"
	"github.com/c3os-io/c3os/cli/machine"
	"github.com/c3os-io/c3os/cli/utils"

	service "github.com/mudler/edgevpn/api/client/service"
)

func Master(cc *config.Config) Role {
	return func(c *service.RoleConfig) error {
		ip := getIP()
		if ip == "" {
			return errors.New("master doesn't have an ip yet")
		}

		r, err := c.Client.Get("role", c.UUID)
		if err != nil || r != "master" {
			// propagate role if we were forced by configuration
			// This unblocks eventual auto instances to try to assign roles
			c.Client.Set("role", c.UUID, "master")
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

		CreateSentinel()

		os.Exit(0)
		return nil
	}
}

// TODO: https://rancher.com/docs/k3s/latest/en/installation/ha-embedded/
func HAMaster(c *service.RoleConfig) {
	c.Logger.Info("HA Role not implemented yet")
}
