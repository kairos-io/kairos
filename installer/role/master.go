package role

import (
	"encoding/base64"
	"errors"
	"io/ioutil"
	"os"
	"strings"

	"github.com/c3os-io/c3os/installer/systemd"
	"github.com/c3os-io/c3os/installer/utils"

	service "github.com/mudler/edgevpn/api/client/service"
)

func Master(c *service.RoleConfig) error {

	ip := getIP()
	if ip == "" {
		return errors.New("master doesn't have an ip yet")
	}

	// Configure k3s service to start on edgevpn0
	c.Logger.Info("Configuring k3s")

	svc, err := systemd.NewService(systemd.WithName("k3s"))
	if err != nil {
		return err
	}

	// Setup systemd unit and starts it
	if err := utils.WriteEnv("/etc/sysconfig/k3s",
		map[string]string{},
	); err != nil {
		return err
	}

	if err := svc.OverrideCmd("/usr/bin/k3s server --flannel-iface=edgevpn0"); err != nil {
		return err
	}

	if err := svc.StartBlocking(); err != nil {
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

// TODO: https://rancher.com/docs/k3s/latest/en/installation/ha-embedded/
func HAMaster(c *service.RoleConfig) {
	c.Logger.Info("HA Role not implemented yet")
}
