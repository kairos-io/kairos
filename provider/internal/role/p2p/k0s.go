package role

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	service "github.com/mudler/edgevpn/api/client/service"
	"gopkg.in/yaml.v3"
)

const (
	K0sDistroName        = "k0s"
	K0sMasterName        = "controller"
	K0sWorkerName        = "worker"
	K0sMasterServiceName = "k0scontroller"
	K0sWorkerServiceName = "k0sworker"
)

// k0sConfigPath is where the p2p role reads and writes k0s's cluster config.
// A var, not a const, so the tests can point it at a temp dir.
var k0sConfigPath = "/etc/k0s/k0s.yaml"

type K0sNode struct {
	providerConfig *providerConfig.Config
	roleConfig     *service.RoleConfig
	ip             string
	role           string
}

func (k *K0sNode) IsWorker() bool {
	return k.role == RoleWorker
}

func (k *K0sNode) K8sBin() string {
	return utils.K0sBin()
}

func (k *K0sNode) DeployKubeVIP() error {
	pconfig := k.ProviderConfig()
	if pconfig.KubeVIP.IsEnabled() {
		return errors.New("KubeVIP is not yet supported with k0s")
	}

	return nil
}

func (k *K0sNode) GenArgs() ([]string, error) {
	var args []string

	if err := ensureK0sConfig(k0sConfigPath); err != nil {
		return args, err
	}
	args = append(args, "--config "+k0sConfigPath)

	data, err := os.ReadFile(k0sConfigPath)
	if err != nil {
		return nil, err
	}

	data, err = applyP2PClusterConfig(data, k.IP())
	if err != nil {
		return args, err
	}

	if err := os.WriteFile(k0sConfigPath, data, 0644); err != nil {
		return args, err
	}

	pconfig := k.ProviderConfig()
	if !pconfig.P2P.UseVPNWithKubernetes() {
		return args, errors.New("having a VPN but not using it for Kubernetes is not yet supported with k0s")
	}

	if pconfig.KubeVIP.IsEnabled() {
		return args, errors.New("KubeVIP is not yet supported with k0s")
	}

	if pconfig.P2P.Auto.HA.ExternalDB != "" {
		return args, errors.New("ExternalDB is not yet supported with k0s")
	}

	if k.HA() && !k.ClusterInit() {
		args = append(args, "--token-file /etc/k0s/token")
	}

	// when we start implementing this functionality, remember to use
	// AppendArgs, and not just return the args here, this is because the
	// function understands if it needs to append or replace the args

	return args, nil
}

// ensureK0sConfig writes k0s's own default cluster config to path, unless a
// config is already there.
//
// A config the user shipped -- through write_files, a yip stage, or a build
// time layer -- is theirs to keep. The p2p role owns three fields in it
// (see applyP2PClusterConfig), not the whole file, so regenerating it
// unconditionally threw away everything else the user asked for.
func ensureK0sConfig(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// Generate into a temporary file and rename it into place, so a k0s that
	// fails half way through leaves no config behind. An empty or truncated
	// one would be indistinguishable, on the next call, from a config the
	// user wrote, and would then be preserved forever.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".k0s.yaml.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck

	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}

	if out, err := utils.SH(fmt.Sprintf("k0s config create > '%s'", tmp.Name())); err != nil {
		return fmt.Errorf("generating a k0s config: %w: %s", err, out)
	}

	return os.Rename(tmp.Name(), path)
}

// applyP2PClusterConfig overrides, in a k0s cluster config, the fields that
// have to follow the edgevpn network instead of k0s's own defaults. Every
// other key is passed through untouched.
func applyP2PClusterConfig(data []byte, ip string) ([]byte, error) {
	var k0sConfig map[string]any
	if err := yaml.Unmarshal(data, &k0sConfig); err != nil {
		return nil, err
	}
	if k0sConfig == nil {
		k0sConfig = map[string]any{}
	}

	// by default k0s uses the first IP address of the machine as the api
	// address, but we want to use the edgevpn IP
	api, err := mappingAt(k0sConfig, "spec", "api")
	if err != nil {
		return nil, err
	}
	api["address"] = ip

	// by default k0s uses the port 8080 for the metrics but this conflicts
	// with the edgevpn API port
	kubeRouter, err := mappingAt(k0sConfig, "spec", "network", "kuberouter")
	if err != nil {
		return nil, err
	}
	kubeRouter["metricsPort"] = 9090

	// just like the api address, we want to use the edgevpn IP for the etcd
	// peer address
	etcd, err := mappingAt(k0sConfig, "spec", "storage", "etcd")
	if err != nil {
		return nil, err
	}
	etcd["peerAddress"] = ip

	return yaml.Marshal(k0sConfig)
}

// mappingAt walks path through nested mappings and returns the one it ends on,
// creating the mappings along the way that are absent. A hand-written config
// carries only the keys its author cared about, so a missing section means
// "k0s default", not "malformed". A section that is present but is not a
// mapping is still an error, reported with the path that reached it.
func mappingAt(root map[string]any, path ...string) (map[string]any, error) {
	current := root
	for i, key := range path {
		switch child := current[key].(type) {
		case map[string]any:
			current = child
		case nil:
			fresh := map[string]any{}
			current[key] = fresh
			current = fresh
		default:
			return nil, fmt.Errorf("k0s config: %s is not a mapping", strings.Join(path[:i+1], "."))
		}
	}

	return current, nil
}

func (k *K0sNode) EnvUnit() string {
	return machine.K0sEnvUnit("k0scontroller")
}

func (k *K0sNode) Service() (machine.Service, error) {
	if k.IsWorker() {
		return machine.K0sWorker()
	}

	return machine.K0s()
}

func (k *K0sNode) Token() (string, error) {
	if k.IsWorker() {
		return k.RoleConfig().Client.Get("workertoken", "token")
	}

	return k.RoleConfig().Client.Get("controllertoken", "token")
}

func (k *K0sNode) GenerateEnv() (env map[string]string) {
	env = make(map[string]string)

	if k.HA() && !k.ClusterInit() {
		nodeToken, _ := k.Token()
		env["K0S_TOKEN"] = nodeToken
	}

	pConfig := k.ProviderConfig()

	if pConfig.K0s.ReplaceEnv {
		env = pConfig.K0s.Env
	} else {
		// Override opts with user-supplied
		for k, v := range pConfig.K0s.Env {
			env[k] = v
		}
	}

	return env
}

func (k *K0sNode) ProviderConfig() *providerConfig.Config {
	return k.providerConfig
}

func (k *K0sNode) SetRoleConfig(c *service.RoleConfig) {
	k.roleConfig = c
}

func (k *K0sNode) RoleConfig() *service.RoleConfig {
	return k.roleConfig
}

func (k *K0sNode) HA() bool {
	return k.role == RoleMasterHA
}

func (k *K0sNode) ClusterInit() bool {
	// k0s does not have a cluster init role like k3s. Instead we should have a way to set in the config
	// if the user wants a single node cluster, multi-node cluster, or HA cluster
	return false
}

func (k *K0sNode) IP() string {
	return k.ip
}

func (k *K0sNode) PropagateData() error {
	c := k.RoleConfig()
	controllerToken, err := utils.SH("k0s token create --role=controller") //nolint:errcheck
	if err != nil {
		c.Logger.Errorf("failed to create controller token: %s", err)
	}

	// we don't want to set the output if there is an error
	if err == nil && controllerToken != "" {
		err := c.Client.Set("controllertoken", "token", strings.TrimSuffix(controllerToken, "\n"))
		if err != nil {
			c.Logger.Error(err)
		}
	}

	workerToken, err := utils.SH("k0s token create --role=worker") //nolint:errcheck
	if err != nil {
		c.Logger.Errorf("failed to create worker token: %s", err)
	}
	// we don't want to set the output if there is an error
	if err == nil && workerToken != "" {
		err := c.Client.Set("workertoken", "token", strings.TrimSuffix(workerToken, "\n"))
		if err != nil {
			c.Logger.Error(err)
		}
	}

	kubeconfig, err := utils.SH("k0s config create") //nolint:errcheck
	if err != nil {
		c.Logger.Error(err)
		return err
	}
	if kubeconfig != "" {
		err := c.Client.Set("kubeconfig", "master", base64.RawURLEncoding.EncodeToString([]byte(kubeconfig)))
		if err != nil {
			c.Logger.Error(err)
		}
	}

	return nil
}

func (k *K0sNode) WorkerArgs() ([]string, error) {
	pconfig := k.ProviderConfig()
	k0sConfig := pconfig.K0sWorker
	args := []string{"--token-file /etc/k0s/token"}

	if k0sConfig.ReplaceArgs {
		args = k0sConfig.Args
	} else {
		args = append(args, k0sConfig.Args...)
	}

	return args, nil
}

func (k *K0sNode) SetupWorker(_, nodeToken string) error {
	if err := os.WriteFile("/etc/k0s/token", []byte(nodeToken), 0644); err != nil {
		return err
	}

	return nil
}

func (k *K0sNode) Role() string {
	if k.IsWorker() {
		return K0sWorkerName
	}

	return K0sMasterName
}

func (k *K0sNode) ServiceName() string {
	if k.IsWorker() {
		return K0sWorkerServiceName
	}

	return K0sMasterServiceName
}

func (k *K0sNode) Env() map[string]string {
	c := k.ProviderConfig()
	if k.IsWorker() {
		return c.K0sWorker.Env
	}

	return c.K0s.Env
}

func (k *K0sNode) Args() []string {
	c := k.ProviderConfig()
	if !c.K0sWorker.IsEnabled() && !c.K0s.IsEnabled() {
		return []string{}
	}

	if k.IsWorker() {
		return c.K0sWorker.Args
	}

	return c.K0s.Args
}

func (k *K0sNode) EnvFile() string {
	return machine.K0sEnvUnit(k.ServiceName())
}

func (k *K0sNode) SetRole(role string) {
	k.role = role
}

func (k *K0sNode) SetIP(ip string) {
	k.ip = ip
}

func (k *K0sNode) GuessInterface() {
	// not used in k0s
}

func (k *K0sNode) Distro() string {
	return K0sDistroName
}
