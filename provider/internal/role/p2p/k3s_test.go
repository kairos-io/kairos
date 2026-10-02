package role

import (
	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("K3sNode Args", func() {
	Context("embedded registry flag", func() {
		It("should include --embedded-registry flag for master when enabled", func() {
			enabled := true
			config := &providerConfig.Config{
				K3s: providerConfig.K3s{
					Enabled:          &enabled,
					EmbeddedRegistry: true,
					Args:             []string{"--existing-arg"},
				},
			}

			node := &K3sNode{
				providerConfig: config,
				role:           "master",
			}

			args := node.Args()

			Expect(args).To(ContainElement("--existing-arg"))
			Expect(args).To(ContainElement("--embedded-registry"))
		})

		It("should not include --embedded-registry flag for worker when enabled", func() {
			enabled := true
			config := &providerConfig.Config{
				K3s: providerConfig.K3s{
					Enabled:          &enabled,
					EmbeddedRegistry: true,
					Args:             []string{"--existing-arg"},
				},
				K3sAgent: providerConfig.K3s{
					Enabled: &enabled,
					Args:    []string{"--worker-arg"},
				},
			}

			node := &K3sNode{
				providerConfig: config,
				role:           "worker",
			}

			args := node.Args()

			Expect(args).To(ContainElement("--worker-arg"))
			Expect(args).NotTo(ContainElement("--embedded-registry"))
		})

		It("should not include --embedded-registry flag when disabled", func() {
			enabled := true
			config := &providerConfig.Config{
				K3s: providerConfig.K3s{
					Enabled:          &enabled,
					EmbeddedRegistry: false,
					Args:             []string{"--existing-arg"},
				},
			}

			node := &K3sNode{
				providerConfig: config,
				role:           "master",
			}

			args := node.Args()

			Expect(args).To(ContainElement("--existing-arg"))
			Expect(args).NotTo(ContainElement("--embedded-registry"))
		})

		It("should preserve existing args when embedded registry is enabled", func() {
			enabled := true
			config := &providerConfig.Config{
				K3s: providerConfig.K3s{
					Enabled:          &enabled,
					EmbeddedRegistry: true,
					Args:             []string{"--arg1", "--arg2"},
				},
			}

			node := &K3sNode{
				providerConfig: config,
				role:           "master",
			}

			args := node.Args()

			Expect(args).To(ContainElement("--arg1"))
			Expect(args).To(ContainElement("--arg2"))
			Expect(args).To(ContainElement("--embedded-registry"))
			Expect(len(args)).To(Equal(3))
		})
	})
})

var _ = Describe("K3sNode GenArgs", func() {
	Context("with an external datastore", func() {
		// p2p.auto.ha.external_db used to replace the argument slice instead of
		// appending to it, so a server started with an external database lost
		// the VPN flannel interface, the kube-vip flags and the embedded
		// registry. The workers keep --flannel-iface=edgevpn0 either way, so
		// the two halves of the cluster ended up on different interfaces.
		enabled := true
		const externalDB = "mysql://user:pass@tcp(1.2.3.4:3306)/k3s"

		configWithExternalDB := func() *providerConfig.Config {
			return &providerConfig.Config{
				P2P: &providerConfig.P2P{
					NetworkToken: "a-token",
					Auto: providerConfig.Auto{
						HA: providerConfig.HA{
							Enable:     &enabled,
							ExternalDB: externalDB,
						},
					},
				},
				K3s: providerConfig.K3s{
					Enabled:          &enabled,
					EmbeddedRegistry: true,
					Args:             []string{"--user-arg"},
				},
				KubeVIP: providerConfig.KubeVIP{
					Enable: &enabled,
					EIP:    "10.0.0.9",
				},
			}
		}

		It("keeps the flags computed before it", func() {
			node := &K3sNode{
				providerConfig: configWithExternalDB(),
				role:           RoleMaster,
				ip:             "10.0.0.9",
				ifaceIP:        "10.1.0.5",
			}

			args, err := node.GenArgs()
			Expect(err).ToNot(HaveOccurred())

			Expect(args).To(ContainElement("--datastore-endpoint=" + externalDB))
			Expect(args).To(ContainElement("--flannel-iface=edgevpn0"))
			Expect(args).To(ContainElement("--tls-san=10.0.0.9"))
			Expect(args).To(ContainElement("--node-ip=10.1.0.5"))
			Expect(args).To(ContainElement("--embedded-registry"))
			Expect(args).To(ContainElement("--user-arg"))
		})

		It("still refuses --cluster-init, which an external datastore replaces", func() {
			node := &K3sNode{
				providerConfig: configWithExternalDB(),
				role:           RoleMasterClusterInit,
				ip:             "10.0.0.9",
				ifaceIP:        "10.1.0.5",
			}

			args, err := node.GenArgs()
			Expect(err).ToNot(HaveOccurred())

			Expect(args).To(ContainElement("--datastore-endpoint=" + externalDB))
			Expect(args).ToNot(ContainElement("--cluster-init"))
		})

		It("uses --cluster-init when no external datastore is set", func() {
			config := configWithExternalDB()
			config.P2P.Auto.HA.ExternalDB = ""

			node := &K3sNode{
				providerConfig: config,
				role:           RoleMasterClusterInit,
				ip:             "10.0.0.9",
				ifaceIP:        "10.1.0.5",
			}

			args, err := node.GenArgs()
			Expect(err).ToNot(HaveOccurred())

			Expect(args).To(ContainElement("--cluster-init"))
			Expect(args).ToNot(ContainElement(HavePrefix("--datastore-endpoint=")))
		})
	})
})
