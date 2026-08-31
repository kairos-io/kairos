package role

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	service "github.com/mudler/edgevpn/api/client/service"
)

const (
	K3sDistroName        = "k3s"
	K3sMasterName        = "server"
	K3sWorkerName        = "agent"
	K3sMasterServiceName = "k3s"
	K3sWorkerServiceName = "k3s-agent"
)

type K3sNode struct {
	providerConfig *providerConfig.Config
	roleConfig     *service.RoleConfig
	ip             string
	iface          string
	ifaceIP        string
	role           string
}

func (k *K3sNode) IsWorker() bool {
	return k.role == RoleWorker
}

func (k *K3sNode) K8sBin() string {
	return utils.K3sBin()
}

func (k *K3sNode) DeployKubeVIP() error {
	pconfig := k.ProviderConfig()
	if !pconfig.KubeVIP.IsEnabled() {
		return nil
	}

	return deployKubeVIP(k.iface, k.ip, pconfig)
}

func (k *K3sNode) GenArgs() ([]string, error) {
	var args []string
	pconfig := k.ProviderConfig()

	if pconfig.P2P.UseVPNWithKubernetes() {
		args = append(args, "--flannel-iface=edgevpn0")
	}

	if pconfig.KubeVIP.IsEnabled() {
		args = append(args, fmt.Sprintf("--tls-san=%s", k.ip), fmt.Sprintf("--node-ip=%s", k.ifaceIP))
	}

	if pconfig.K3s.EmbeddedRegistry {
		args = append(args, "--embedded-registry")
	}

	if pconfig.P2P.Auto.HA.ExternalDB != "" {
		args = []string{fmt.Sprintf("--datastore-endpoint=%s", pconfig.P2P.Auto.HA.ExternalDB)}
	}

	if k.HA() && !k.ClusterInit() {
		clusterInitIP, _ := k.roleConfig.Client.Get("master", "ip")
		args = append(args, fmt.Sprintf("--server=https://%s:6443", clusterInitIP))
	}
	// The --cluster-init flag changes the embedded SQLite DB to etcd. We don't
	// want to do this if we're using an external DB.
	if k.ClusterInit() && pconfig.P2P.Auto.HA.ExternalDB == "" {
		args = append(args, "--cluster-init")
	}

	args = k.AppendArgs(args)

	return args, nil
}

func (k *K3sNode) AppendArgs(other []string) []string {
	c := k.ProviderConfig()
	if c.K3s.ReplaceArgs {
		return c.K3s.Args
	}

	return append(other, c.K3s.Args...)
}

func (k *K3sNode) EnvUnit() string {
	return machine.K3sEnvUnit("k3s")
}

func (k *K3sNode) Service() (machine.Service, error) {
	if k.role == "worker" {
		return machine.K3sAgent()
	}

	return machine.K3s()
}

func (k *K3sNode) Token() (string, error) {
	return k.RoleConfig().Client.Get("nodetoken", "token")
}

func (k *K3sNode) GenerateEnv() (env map[string]string) {
	env = make(map[string]string)

	if k.HA() && !k.ClusterInit() {
		nodeToken, _ := k.Token()
		env["K3S_TOKEN"] = nodeToken
	}

	pConfig := k.ProviderConfig()

	if pConfig.K3s.ReplaceEnv {
		env = pConfig.K3s.Env
	} else {
		// Override opts with user-supplied
		for k, v := range pConfig.K3s.Env {
			env[k] = v
		}
	}

	return env
}

func (k *K3sNode) ProviderConfig() *providerConfig.Config {
	return k.providerConfig
}

func (k *K3sNode) SetRoleConfig(c *service.RoleConfig) {
	k.roleConfig = c
}

func (k *K3sNode) RoleConfig() *service.RoleConfig {
	return k.roleConfig
}

func (k *K3sNode) HA() bool {
	return k.role == "master/ha"
}

func (k *K3sNode) ClusterInit() bool {
	return k.role == "master/clusterinit"
}

func (k *K3sNode) IP() string {
	return k.ip
}

func (k *K3sNode) PropagateData() error {
	c := k.RoleConfig()
	tokenB, err := os.ReadFile("/var/lib/rancher/k3s/server/node-token")
	if err != nil {
		c.Logger.Error(err)
		return err
	}

	nodeToken := string(tokenB)
	nodeToken = strings.TrimRight(nodeToken, "\n")
	if nodeToken != "" {
		err := c.Client.Set("nodetoken", "token", nodeToken)
		if err != nil {
			c.Logger.Error(err)
		}
	}

	kubeB, err := os.ReadFile("/etc/rancher/k3s/k3s.yaml")
	if err != nil {
		c.Logger.Error(err)
		return err
	}
	kubeconfig := string(kubeB)
	if kubeconfig != "" {
		err := c.Client.Set("kubeconfig", "master", base64.RawURLEncoding.EncodeToString(kubeB))
		if err != nil {
			c.Logger.Error(err)
		}
	}

	return nil
}

func (k *K3sNode) WorkerArgs() ([]string, error) {
	pconfig := k.ProviderConfig()
	k3sConfig := providerConfig.K3s{}
	if pconfig.K3sAgent.IsEnabled() {
		k3sConfig = pconfig.K3sAgent
	}

	args := []string{
		"--with-node-id",
	}

	if pconfig.P2P.UseVPNWithKubernetes() {
		ip := utils.GetInterfaceIP("edgevpn0")
		if ip == "" {
			return nil, errors.New("node doesn't have an ip yet")
		}
		args = append(args,
			fmt.Sprintf("--node-ip %s", ip),
			"--flannel-iface=edgevpn0")
	} else {
		iface := guessInterface(pconfig)
		ip := utils.GetInterfaceIP(iface)
		args = append(args,
			fmt.Sprintf("--node-ip %s", ip))
	}

	if k3sConfig.ReplaceArgs {
		args = k3sConfig.Args
	} else {
		args = append(args, k3sConfig.Args...)
	}

	return args, nil
}

func (k *K3sNode) SetupWorker(masterIP, nodeToken string) error {
	pconfig := k.ProviderConfig()

	nodeToken = strings.TrimRight(nodeToken, "\n")

	k3sConfig := providerConfig.K3s{}
	if pconfig.K3sAgent.IsEnabled() {
		k3sConfig = pconfig.K3sAgent
	}

	env := map[string]string{
		"K3S_URL":   fmt.Sprintf("https://%s:6443", masterIP),
		"K3S_TOKEN": nodeToken,
	}

	if k3sConfig.ReplaceEnv {
		env = k3sConfig.Env
	} else {
		// Override opts with user-supplied
		for k, v := range k3sConfig.Env {
			env[k] = v
		}
	}

	if err := utils.WriteEnv(machine.K3sEnvUnit("k3s-agent"),
		env,
	); err != nil {
		return err
	}

	return nil
}

func (k *K3sNode) Role() string {
	if k.IsWorker() {
		return K3sWorkerName
	}

	return K3sMasterName
}

func (k *K3sNode) ServiceName() string {
	if k.IsWorker() {
		return K3sWorkerServiceName
	}

	return K3sMasterServiceName
}

func (k *K3sNode) Env() map[string]string {
	c := k.ProviderConfig()
	if k.IsWorker() {
		return c.K3sAgent.Env
	}

	return c.K3s.Env
}

func (k *K3sNode) Args() []string {
	c := k.ProviderConfig()
	var args []string

	if !c.K3sAgent.IsEnabled() && !c.K3s.IsEnabled() {
		return []string{}
	}

	if k.IsWorker() {
		return c.K3sAgent.Args
	}

	args = c.K3s.Args
	// Add embedded registry flag if enabled (for non-p2p mode, server only)
	if c.K3s.EmbeddedRegistry {
		args = append(args, "--embedded-registry")
	}

	return args
}

func (k *K3sNode) EnvFile() string {
	return machine.K3sEnvUnit(k.ServiceName())
}

func (k *K3sNode) SetRole(role string) {
	k.role = role
}

func (k *K3sNode) SetIP(ip string) {
	k.ip = ip
}

func (k *K3sNode) GuessInterface() {
	iface := guessInterface(k.ProviderConfig())
	ifaceIP := utils.GetInterfaceIP(iface)

	k.iface = iface
	k.ifaceIP = ifaceIP
}

func (k *K3sNode) Distro() string {
	return K3sDistroName
}
