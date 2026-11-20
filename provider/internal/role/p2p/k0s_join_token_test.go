package role

import (
	"os"
	"path/filepath"

	providerConfig "github.com/kairos-io/kairos/v4/provider/internal/provider/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("the k0s join token", func() {
	Describe("GenerateEnv", func() {
		It("does not hand an HA controller the token a second time", func() {
			// k0s counts --token-file and K0S_TOKEN as two token sources and
			// refuses to start when both are set. GenArgs owns the file, so
			// the environment must stay out of it.
			node := &K0sNode{providerConfig: &providerConfig.Config{}}
			node.SetRole(RoleMasterHA)

			Expect(node.HA()).To(BeTrue())
			Expect(node.GenerateEnv()).NotTo(HaveKey("K0S_TOKEN"))
		})

		It("still carries the environment the user asked for", func() {
			node := &K0sNode{providerConfig: &providerConfig.Config{
				K0s: providerConfig.K0s{Env: map[string]string{"HTTP_PROXY": "http://proxy:3128"}},
			}}
			node.SetRole(RoleMasterHA)

			Expect(node.GenerateEnv()).To(HaveKeyWithValue("HTTP_PROXY", "http://proxy:3128"))
		})
	})

	Describe("writeK0sJoinToken", func() {
		var path string

		BeforeEach(func() {
			path = filepath.Join(GinkgoT().TempDir(), "token")
		})

		It("writes the token where k0s reads it back from", func() {
			Expect(writeK0sJoinToken(path, "a-join-token")).To(Succeed())

			Expect(os.ReadFile(path)).To(BeEquivalentTo("a-join-token"))
		})

		It("keeps the token out of reach of everyone but root", func() {
			Expect(writeK0sJoinToken(path, "a-join-token")).To(Succeed())

			info, err := os.Stat(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		It("tightens a token an older Kairos left world readable", func() {
			// os.WriteFile keeps the mode of a file it truncates, so a node
			// that joined before this was fixed would keep its 0644 token.
			Expect(os.WriteFile(path, []byte("old-token"), 0644)).To(Succeed())

			Expect(writeK0sJoinToken(path, "new-token")).To(Succeed())

			info, err := os.Stat(path)
			Expect(err).ToNot(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
			Expect(os.ReadFile(path)).To(BeEquivalentTo("new-token"))
		})

		It("refuses an empty token rather than letting k0s fail on it", func() {
			Expect(writeK0sJoinToken(path, "")).To(MatchError(ContainSubstring("empty k0s join token")))

			_, err := os.Stat(path)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})
	})
})
