package clusterplugin

type Role string

func (n *Role) MarshalYAML() (interface{}, error) {
	return string(*n), nil
}

func (n *Role) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var val string
	if err := unmarshal(&val); err != nil {
		return err
	}

	*n = Role(val)

	return nil
}

const (
	// RoleInit denotes a special `RoleControlPlane` that can run special tasks to initialize the cluster.  There will only ever be one node with this role in a cluster.
	RoleInit = "init"

	// RoleControlPlane denotes nodes that persist cluster information and host the kubernetes control plane
	RoleControlPlane = "controlplane"

	// RoleWorker denotes a node that runs workloads in the cluster
	RoleWorker = "worker"
)

type Cluster struct {
	// ClusterToken is a unique string that can be used to distinguish different clusters on networks with multiple clusters
	ClusterToken string `yaml:"cluster_token,omitempty"`

	// ControlPlaneHost is a host that all nodes can resolve and use for node registration
	ControlPlaneHost string `yaml:"control_plane_host,omitempty"`

	// Role informs the sdk what kind of installation to manage on this device
	Role Role `yaml:"role,omitempty"`

	// Options are arbitrary values the sdk may be interested in.  These values are not validated by C3OS and are simply forwarded to the sdk
	Options string `yaml:"config,omitempty"`
}

type Config struct {
	Cluster *Cluster `yaml:"cluster,omitempty"`
}
