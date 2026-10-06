package role

import (
	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The p2p roles build a k3s or k0s command line from arguments they generate
// plus the `args:` the matching cloud-config block carries. Both nodes have to
// agree on what `args:` and `replace_args:` mean: a k0s controller used to drop
// both, so a documented `args: ["--enable-worker"]` never reached k0s
// (kairos-io/kairos#5257).
var _ = Describe("the args: a Kubernetes block carries", func() {
	enabled := true

	// nodes returns one node per distribution, each configured so that its own
	// server block carries userArgs and the given replace_args setting.
	nodes := func(userArgs []string, replace bool) map[string]K8sNode {
		return map[string]K8sNode{
			"k3s": &K3sNode{
				role: RoleMaster,
				providerConfig: &providerConfig.Config{
					K3s: providerConfig.K3s{
						Enabled: &enabled, Args: userArgs, ReplaceArgs: replace,
					},
				},
			},
			"k0s": &K0sNode{
				role: RoleMaster,
				providerConfig: &providerConfig.Config{
					K0s: providerConfig.K0s{
						Enabled: &enabled, Args: userArgs, ReplaceArgs: replace,
					},
				},
			},
		}
	}

	It("reaches the command line of both distributions", func() {
		for distro, node := range nodes([]string{"--enable-worker"}, false) {
			args := node.AppendArgs([]string{"--generated"})

			Expect(args).To(ContainElement("--enable-worker"), distro)
			Expect(args).To(ContainElement("--generated"), distro)
		}
	})

	It("replaces the generated arguments when replace_args is set", func() {
		for distro, node := range nodes([]string{"--single"}, true) {
			args := node.AppendArgs([]string{"--generated"})

			Expect(args).To(Equal([]string{"--single"}), distro)
		}
	})

	It("leaves the generated arguments alone when the block carries none", func() {
		for distro, node := range nodes(nil, false) {
			args := node.AppendArgs([]string{"--generated"})

			Expect(args).To(Equal([]string{"--generated"}), distro)
		}
	})

	// replace_args with an empty args: is the one case where the two readings
	// differ. It means "run the bare command", not "ignore me".
	It("empties the command line when replace_args is set and args: is empty", func() {
		for distro, node := range nodes(nil, true) {
			Expect(node.AppendArgs([]string{"--generated"})).To(BeEmpty(), distro)
		}
	})
})

var _ = Describe("the args: a worker block carries", func() {
	enabled := true

	It("reaches the k0s worker command line, next to the token file", func() {
		node := &K0sNode{
			role: RoleWorker,
			providerConfig: &providerConfig.Config{
				K0sWorker: providerConfig.K0s{
					Enabled: &enabled, Args: []string{"--labels=role=edge"},
				},
			},
		}

		args, err := node.WorkerArgs()

		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(Equal([]string{"--token-file /etc/k0s/token", "--labels=role=edge"}))
	})

	// The k3s worker arm reads an interface IP, so the interface is named
	// after one that cannot exist: GetInterfaceIP then returns the empty
	// string and the generated part of the command line is fixed.
	It("reaches the k3s worker command line, after the generated arguments", func() {
		no := false
		node := &K3sNode{
			role: RoleWorker,
			providerConfig: &providerConfig.Config{
				P2P:     &providerConfig.P2P{VPN: providerConfig.VPN{Use: &no}},
				KubeVIP: providerConfig.KubeVIP{Interface: "no-such-iface0"},
				K3sAgent: providerConfig.K3s{
					Enabled: &enabled, Args: []string{"--labels=role=edge"},
				},
			},
		}

		args, err := node.WorkerArgs()

		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(Equal([]string{"--with-node-id", "--node-ip ", "--labels=role=edge"}))
	})

	It("replaces the k0s token file argument when replace_args is set", func() {
		node := &K0sNode{
			role: RoleWorker,
			providerConfig: &providerConfig.Config{
				K0sWorker: providerConfig.K0s{
					Enabled: &enabled, Args: []string{"--token-file /mine"}, ReplaceArgs: true,
				},
			},
		}

		args, err := node.WorkerArgs()

		Expect(err).ToNot(HaveOccurred())
		Expect(args).To(Equal([]string{"--token-file /mine"}))
	})
})
