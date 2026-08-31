package role

import (
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// MockBinaryDetector for testing
type MockBinaryDetector struct {
	k3sBin string
	k0sBin string
}

func (m *MockBinaryDetector) K3sBin() string {
	return m.k3sBin
}

func (m *MockBinaryDetector) K0sBin() string {
	return m.k0sBin
}

var _ = Describe("NewK8sNode", func() {
	Context("explicit k8s configuration", func() {
		It("should return error when k3s is explicitly disabled", func() {
			enabled := false
			config := &providerConfig.Config{
				K3s: providerConfig.K3s{
					Enabled: &enabled,
				},
			}

			// Mock k3s binary as available so we can test configuration logic
			mock := &MockBinaryDetector{k3sBin: "/usr/bin/k3s", k0sBin: ""}
			_, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no k8s configuration found"))
		})

		It("should return error when k3s-agent is explicitly disabled", func() {
			enabled := false
			config := &providerConfig.Config{
				K3sAgent: providerConfig.K3s{
					Enabled: &enabled,
				},
			}

			// Mock k3s binary as available so we can test configuration logic
			mock := &MockBinaryDetector{k3sBin: "/usr/bin/k3s", k0sBin: ""}
			_, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no k8s configuration found"))
		})

		It("should return error when k0s is explicitly disabled", func() {
			enabled := false
			config := &providerConfig.Config{
				K0s: providerConfig.K0s{
					Enabled: &enabled,
				},
			}

			// Mock k0s binary as available so we can test configuration logic
			mock := &MockBinaryDetector{k3sBin: "", k0sBin: "/usr/bin/k0s"}
			_, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no k8s configuration found"))
		})

		It("should return error when k0s-worker is explicitly disabled", func() {
			enabled := false
			config := &providerConfig.Config{
				K0sWorker: providerConfig.K0s{
					Enabled: &enabled,
				},
			}

			// Mock k0s binary as available so we can test configuration logic
			mock := &MockBinaryDetector{k3sBin: "", k0sBin: "/usr/bin/k0s"}
			_, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no k8s configuration found"))
		})
	})

	Context("p2p configuration with specific role", func() {
		It("should return error for invalid p2p.role", func() {
			config := &providerConfig.Config{
				P2P: &providerConfig.P2P{
					Role: "invalid",
				},
			}

			// Mock k3s binary as available so we can test configuration logic
			mock := &MockBinaryDetector{k3sBin: "/usr/bin/k3s", k0sBin: ""}
			_, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid p2p.role specified"))
		})
	})

	Context("conflicting configurations", func() {
		It("should use p2p.role when both p2p.auto.enabled=true and p2p.role are specified", func() {
			enabled := true
			config := &providerConfig.Config{
				P2P: &providerConfig.P2P{
					Role: RoleMaster,
					Auto: providerConfig.Auto{
						Enable: &enabled,
					},
				},
			}

			// Mock k3s binary as available so we can test configuration logic
			mock := &MockBinaryDetector{k3sBin: "/usr/bin/k3s", k0sBin: ""}
			node, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(BeNil())
			Expect(node).ToNot(BeNil())
			// The role should be set to the explicit role, not auto-assigned
			Expect(node.(*K3sNode).role).To(Equal(RoleMaster))
		})
	})

	Context("no k8s configuration", func() {
		It("should return error when no k8s configuration is provided", func() {
			config := &providerConfig.Config{}

			// Mock no binaries available to test the binary check
			mock := &MockBinaryDetector{k3sBin: "", k0sBin: ""}
			_, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no k8s binary is available"))
		})

		It("should create auto-mode node when p2p is configured with network token", func() {
			config := &providerConfig.Config{
				P2P: &providerConfig.P2P{
					NetworkToken: "fooblar",
				},
			}

			// Mock k3s binary available to test configuration logic
			mock := &MockBinaryDetector{k3sBin: "/usr/bin/k3s", k0sBin: ""}
			node, err := NewK8sNodeWithDetector(config, mock)
			Expect(err).To(BeNil())
			Expect(node).ToNot(BeNil())
			// Node should be created in auto mode (no explicit role set)
			Expect(node.(*K3sNode).role).To(Equal(""))
		})
	})
})
