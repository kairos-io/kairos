package role

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	"github.com/kairos-io/kairos/v4/provider/internal/services"
	initsvc "github.com/kairos-io/kairos/v4/sdk/machine/service"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The worker path writes the node's environment the way the master path and
// the non-p2p path do. Before this was wired up a k0s worker had no writer at
// all, so k0s-worker.env was accepted and then dropped: k3s got away with it
// because K3sNode.SetupWorker writes an env file for K3S_URL and K3S_TOKEN
// anyway. See kairos-io/kairos#5311.
var _ = Describe("worker service environment", func() {
	enabled := true

	// rootedService builds the service a worker of this distro gets, writing
	// under root rather than onto the host, and answers with the file the unit
	// reads at runtime.
	rootedService := func(flavor initsvc.Flavor, spec initsvc.Spec, root string) (initsvc.Service, string) {
		spec.Root = root
		svc, err := initsvc.NewFor(flavor, spec)
		Expect(err).ToNot(HaveOccurred())

		return svc, filepath.Join(root, spec.For(flavor).EnvFile)
	}

	for _, flavor := range initsvc.Flavors() {
		Context("on "+string(flavor), func() {
			It("gives a k0s worker the variables under k0s-worker.env", func() {
				root := GinkgoT().TempDir()
				node := &K0sNode{
					role: RoleWorker,
					providerConfig: &providerConfig.Config{
						K0sWorker: providerConfig.K0s{
							Enabled: &enabled,
							Env:     map[string]string{"HTTPS_PROXY": "http://proxy.example:3128"},
						},
					},
				}

				svc, envFile := rootedService(flavor, services.K0sSpec(services.K0sWorkerServiceName), root)
				Expect(writeWorkerEnv(svc, node)).To(Succeed())

				Expect(godotenv.Read(envFile)).To(HaveKeyWithValue("HTTPS_PROXY", "http://proxy.example:3128"))
			})

			It("gives a k3s worker the variables under k3s-agent.env", func() {
				root := GinkgoT().TempDir()
				node := &K3sNode{
					role: RoleWorker,
					providerConfig: &providerConfig.Config{
						K3sAgent: providerConfig.K3s{
							Enabled: &enabled,
							Env:     map[string]string{"HTTPS_PROXY": "http://proxy.example:3128"},
						},
					},
				}

				svc, envFile := rootedService(flavor, services.K3sSpec(K3sWorkerServiceName), root)
				Expect(writeWorkerEnv(svc, node)).To(Succeed())

				Expect(godotenv.Read(envFile)).To(HaveKeyWithValue("HTTPS_PROXY", "http://proxy.example:3128"))
			})

			It("does not give a worker the controller's environment", func() {
				root := GinkgoT().TempDir()
				node := &K0sNode{
					role: RoleWorker,
					providerConfig: &providerConfig.Config{
						K0s: providerConfig.K0s{
							Enabled: &enabled,
							Env:     map[string]string{"HTTPS_PROXY": "http://proxy.example:3128"},
						},
					},
				}

				svc, envFile := rootedService(flavor, services.K0sSpec(services.K0sWorkerServiceName), root)
				Expect(writeWorkerEnv(svc, node)).To(Succeed())

				Expect(envFile).ToNot(BeAnExistingFile())
			})

			It("leaves the env file alone when the block declares no environment", func() {
				root := GinkgoT().TempDir()
				node := &K0sNode{
					role:           RoleWorker,
					providerConfig: &providerConfig.Config{K0sWorker: providerConfig.K0s{Enabled: &enabled}},
				}

				svc, envFile := rootedService(flavor, services.K0sSpec(services.K0sWorkerServiceName), root)
				Expect(os.MkdirAll(filepath.Dir(envFile), 0755)).To(Succeed())
				Expect(os.WriteFile(envFile, []byte("command_args=\"'worker' --token-file /etc/k0s/token\"\n"), 0600)).To(Succeed())

				Expect(writeWorkerEnv(svc, node)).To(Succeed())

				Expect(godotenv.Read(envFile)).To(HaveKey("command_args"))
			})
		})
	}
})
