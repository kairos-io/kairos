package role

import (
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	"github.com/kube-vip/kube-vip/pkg/kubevip"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("generateKubeVIP image", func() {
	BeforeEach(func() {
		// generateKubeVIP mutates package-level state — reset between specs.
		initConfig = kubevip.Config{}
		initLoadBalancer = kubevip.LoadBalancer{}
	})

	It("uses the default image when KubeVIP.Image is unset", func() {
		cfg := &providerConfig.Config{KubeVIP: providerConfig.KubeVIP{}}

		out, err := generateKubeVIP("daemonset", "eth0", "192.168.1.1", cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring(DefaultKubeVIPImage + ":" + DefaultKubeVIPVersion))
	})

	It("uses the configured image when KubeVIP.Image is set", func() {
		cfg := &providerConfig.Config{
			KubeVIP: providerConfig.KubeVIP{
				Image: "my-registry.example.com/kube-vip/kube-vip",
			},
		}

		out, err := generateKubeVIP("daemonset", "eth0", "192.168.1.1", cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("my-registry.example.com/kube-vip/kube-vip:" + DefaultKubeVIPVersion))
		Expect(out).NotTo(ContainSubstring(DefaultKubeVIPImage + ":"))
	})

	It("respects the configured Version alongside a custom Image", func() {
		cfg := &providerConfig.Config{
			KubeVIP: providerConfig.KubeVIP{
				Image:   "my-registry.example.com/kube-vip/kube-vip",
				Version: "v1.0.0",
			},
		}

		out, err := generateKubeVIP("pod", "eth0", "192.168.1.1", cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("my-registry.example.com/kube-vip/kube-vip:v1.0.0"))
	})
})
