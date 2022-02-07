package role

import (
	"encoding/base64"
	"errors"
	"io/ioutil"
	"os"
	"strings"

	"github.com/mudler/c3os/installer/systemd"
	service "github.com/mudler/edgevpn/api/client/service"
)

const (
	ProcessK3sMaster = "k3smaster"
)

func Master(c *service.RoleConfig) error {

	ip := getIP()
	if ip == "" {
		return errors.New("master doesn't have an ip yet")
	}

	// Configure k3s service to start on edgevpn0
	c.Logger.Info("Configuring k3s")

	// Setup systemd unit and starts it
	if err := systemd.WriteEnv("/etc/systemd/system/k3s.service.env",
		map[string]string{},
	); err != nil {
		return err
	}

	if err := systemd.OverrideServiceCmd("k3s", "/usr/bin/k3s server --flannel-iface=edgevpn0"); err != nil {
		return err
	}

	if err := systemd.StartUnitBlocking("k3s"); err != nil {
		return err
	}

	if err := systemd.EnableUnit("k3s"); err != nil {
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
