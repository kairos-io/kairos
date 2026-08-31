package role

import (
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
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
