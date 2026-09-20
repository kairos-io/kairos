package role

import (
	"os"
	"path/filepath"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("K0sNode worker setup", func() {
	var dir, tokenFile, envFile string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		tokenFile = filepath.Join(dir, "token")
		envFile = filepath.Join(dir, "k0sworker")
	})

	It("writes the k0s-worker environment next to the join token", func() {
		node := &K0sNode{
			providerConfig: &providerConfig.Config{
				K0sWorker: providerConfig.K0s{
					Env: map[string]string{
						"HTTPS_PROXY": "http://proxy.example:3128",
						"K0S_EXTRA":   "yes",
					},
				},
			},
			role: RoleWorker,
		}

		Expect(node.setupWorker(tokenFile, envFile, "a-join-token\n")).To(Succeed())

		token, err := os.ReadFile(tokenFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(token)).To(Equal("a-join-token\n"))

		env, err := os.ReadFile(envFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(env)).To(ContainSubstring("HTTPS_PROXY="))
		Expect(string(env)).To(ContainSubstring("proxy.example:3128"))
		Expect(string(env)).To(ContainSubstring("K0S_EXTRA="))
	})

	It("takes the environment from the worker block, not the controller one", func() {
		node := &K0sNode{
			providerConfig: &providerConfig.Config{
				K0s: providerConfig.K0s{
					Env: map[string]string{"ONLY_ON_CONTROLLER": "1"},
				},
				K0sWorker: providerConfig.K0s{
					Env: map[string]string{"ONLY_ON_WORKER": "1"},
				},
			},
			role: RoleWorker,
		}

		Expect(node.setupWorker(tokenFile, envFile, "a-join-token")).To(Succeed())

		env, err := os.ReadFile(envFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(env)).To(ContainSubstring("ONLY_ON_WORKER="))
		Expect(string(env)).ToNot(ContainSubstring("ONLY_ON_CONTROLLER"))
	})

	It("sends the environment to the worker unit's file, not the controller's", func() {
		node := &K0sNode{providerConfig: &providerConfig.Config{}, role: RoleWorker}

		Expect(node.EnvFile()).To(HaveSuffix(K0sWorkerServiceName))
		Expect(node.ServiceName()).To(Equal(K0sWorkerServiceName))
	})

	It("still fails when the token cannot be written", func() {
		node := &K0sNode{providerConfig: &providerConfig.Config{}, role: RoleWorker}

		err := node.setupWorker(filepath.Join(dir, "missing", "token"), envFile, "a-join-token")
		Expect(err).To(HaveOccurred())
	})
})
