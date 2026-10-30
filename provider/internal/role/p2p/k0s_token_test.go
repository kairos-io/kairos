package role

import (
	"os"
	"path/filepath"
	"strings"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The file SetupWorker writes is the whole credential for joining the cluster:
// k0s reads it back through "--token-file", as root, and nothing else on the
// node needs the bytes. See kairos-io/kairos#4772, and #4761 for the env files
// that carry the same class of secret.
var _ = Describe("K0sNode SetupWorker", func() {
	var node *K0sNode
	var tokenPath string
	var originalPath string

	BeforeEach(func() {
		originalPath = k0sTokenPath
		tokenPath = filepath.Join(GinkgoT().TempDir(), "token")
		k0sTokenPath = tokenPath
		node = &K0sNode{providerConfig: &providerConfig.Config{}}
	})

	AfterEach(func() {
		k0sTokenPath = originalPath
	})

	It("writes the join token readable by root only", func() {
		Expect(node.SetupWorker("10.0.0.1", "a-join-token")).To(Succeed())

		info, err := os.Stat(tokenPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)),
			"mode %04o exposes the k0s join token to every local user", info.Mode().Perm())

		content, err := os.ReadFile(tokenPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("a-join-token"))
	})

	// A node that joined before this was fixed already has a 0644 file, and
	// os.WriteFile keeps the mode of a file it truncates. Without an explicit
	// chmod the token stays exposed for the life of the node.
	It("tightens a token file that is already on disk", func() {
		Expect(os.WriteFile(tokenPath, []byte("an-old-token"), 0644)).To(Succeed())

		Expect(node.SetupWorker("10.0.0.1", "a-new-token")).To(Succeed())

		info, err := os.Stat(tokenPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)),
			"mode %04o left the k0s join token exposed after a rejoin", info.Mode().Perm())

		content, err := os.ReadFile(tokenPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(content)).To(Equal("a-new-token"))
	})

	// Pins the two halves together: the mode above only protects anything for
	// as long as this is the file k0s is told to read.
	It("hands k0s the same path it wrote", func() {
		args, err := node.WorkerArgs()
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Join(args, " ")).To(ContainSubstring("--token-file " + tokenPath))
	})
})
