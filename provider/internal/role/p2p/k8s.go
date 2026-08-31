package role

import (
	"errors"

	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	service "github.com/mudler/edgevpn/api/client/service"
)

// BinaryDetector interface for detecting k8s binaries.
type BinaryDetector interface {
	K3sBin() string
	K0sBin() string
}

// DefaultBinaryDetector uses the utils package to detect binaries.
type DefaultBinaryDetector struct{}

func (d *DefaultBinaryDetector) K3sBin() string {
	return utils.K3sBin()
}

func (d *DefaultBinaryDetector) K0sBin() string {
	return utils.K0sBin()
}

type K8sNode interface {
	PropagateData() error
	IP() string
	ClusterInit() bool
	HA() bool
	ProviderConfig() *providerConfig.Config
	SetRoleConfig(c *service.RoleConfig)
	RoleConfig() *service.RoleConfig
	GenerateEnv() map[string]string
	Service() (machine.Service, error)
	EnvUnit() string
	GenArgs() ([]string, error)
	DeployKubeVIP() error
	Token() (string, error)
	K8sBin() string
	SetupWorker(masterIP, nodeToken string) error
	Role() string
	WorkerArgs() ([]string, error)
	ServiceName() string
	Env() map[string]string
	Args() []string
	EnvFile() string
	SetRole(role string)
	SetIP(ip string)
	GuessInterface()
	Distro() string
}

// NewK8sNodeWithDetector creates a new K8sNode with a custom binary detector.
func NewK8sNodeWithDetector(c *providerConfig.Config, detector BinaryDetector) (K8sNode, error) {
	k3sBinAvailable := detector.K3sBin() != ""
	k0sBinAvailable := detector.K0sBin() != ""

	if !k3sBinAvailable && !k0sBinAvailable {
		return nil, errors.New("no k8s binary is available")
	}

	if c.K3s.IsEnabled() {
		return &K3sNode{providerConfig: c, role: RoleMaster}, nil
	}
	if c.K3sAgent.IsEnabled() {
		return &K3sNode{providerConfig: c, role: RoleWorker}, nil
	}
	if c.K0s.IsEnabled() {
		return &K0sNode{providerConfig: c, role: RoleMaster}, nil
	}
	if c.K0sWorker.IsEnabled() {
		return &K0sNode{providerConfig: c, role: RoleWorker}, nil
	}

	if !c.IsP2PConfigured() {
		return nil, errors.New("no k8s configuration found. To enable k8s, either: 1) explicitly enable k3s, k3s-agent, k0s, or k0s-worker or 2) configure p2p with a network token")
	}

	if c.P2P.Role != "" {
		if c.P2P.Role != RoleMaster && c.P2P.Role != RoleWorker {
			return nil, errors.New("invalid p2p.role specified, must be 'master' or 'worker'")
		}

		if k3sBinAvailable {
			return &K3sNode{providerConfig: c, role: c.P2P.Role}, nil
		}

		if k0sBinAvailable {
			return &K0sNode{providerConfig: c, role: c.P2P.Role}, nil
		}
	}

	if c.P2P.IsAutoEnabled() {
		if k3sBinAvailable {
			return &K3sNode{providerConfig: c}, nil // No role set, will be assigned automatically
		}
		if k0sBinAvailable {
			return &K0sNode{providerConfig: c}, nil // No role set, will be assigned automatically
		}
	}

	return nil, errors.New("no k8s configuration found but p2p is configured")
}

// NewK8sNode creates a new K8sNode using the default binary detector
// This is the convenience function for production use.
func NewK8sNode(c *providerConfig.Config) (K8sNode, error) {
	return NewK8sNodeWithDetector(c, &DefaultBinaryDetector{})
}
