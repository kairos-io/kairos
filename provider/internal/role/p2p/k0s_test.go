package role

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

var _ = Describe("K0sNode p2p cluster config", func() {
	// specAt digs out a nested mapping so the expectations below read as
	// paths rather than as chains of type assertions.
	specAt := func(data []byte, path ...string) map[string]any {
		var root map[string]any
		Expect(yaml.Unmarshal(data, &root)).To(Succeed())

		current := root
		for _, key := range path {
			child, ok := current[key].(map[string]any)
			Expect(ok).To(BeTrue(), "expected a mapping at %q in:\n%s", key, data)
			current = child
		}

		return current
	}

	Describe("applyP2PClusterConfig", func() {
		// This is the shape `k0s config create` produces, and the case that
		// used to fail outright: the override walk type-asserted every
		// section to map[any]any, which is what yaml.v2 decodes a nested
		// mapping into. yaml.v3, which this repo uses, decodes it into
		// map[string]any, so the very first assertion missed and every
		// bootstrap died on "k0s config does not have a spec".
		It("points the api, kube-router and etcd at the vpn", func() {
			out, err := applyP2PClusterConfig([]byte(`
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
spec:
  api:
    address: 192.168.1.10
  network:
    kuberouter:
      metricsPort: 8080
  storage:
    etcd:
      peerAddress: 192.168.1.10
`), "10.1.0.1")
			Expect(err).ToNot(HaveOccurred())

			Expect(specAt(out, "spec", "api")).To(HaveKeyWithValue("address", "10.1.0.1"))
			Expect(specAt(out, "spec", "network", "kuberouter")).To(HaveKeyWithValue("metricsPort", 9090))
			Expect(specAt(out, "spec", "storage", "etcd")).To(HaveKeyWithValue("peerAddress", "10.1.0.1"))
		})

		It("keeps the keys it does not own", func() {
			out, err := applyP2PClusterConfig([]byte(`
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  api:
    sans:
      - k0s.test
`), "10.1.0.1")
			Expect(err).ToNot(HaveOccurred())

			// The reporter's config in #4285: only spec.api.sans. It has to
			// survive, and the overrides have to land next to it.
			Expect(specAt(out, "spec", "api")).To(HaveKeyWithValue("sans", []any{"k0s.test"}))
			Expect(specAt(out, "spec", "api")).To(HaveKeyWithValue("address", "10.1.0.1"))
			Expect(specAt(out, "metadata")).To(HaveKeyWithValue("name", "k0s"))
			Expect(specAt(out)).To(HaveKeyWithValue("kind", "ClusterConfig"))
		})

		It("creates the sections a partial config leaves out", func() {
			out, err := applyP2PClusterConfig([]byte(`
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
spec:
  api:
    sans:
      - k0s.test
`), "10.1.0.1")
			Expect(err).ToNot(HaveOccurred())

			// spec.network and spec.storage are absent above. Erroring out
			// there is what made a hand-written config unusable.
			Expect(specAt(out, "spec", "network", "kuberouter")).To(HaveKeyWithValue("metricsPort", 9090))
			Expect(specAt(out, "spec", "storage", "etcd")).To(HaveKeyWithValue("peerAddress", "10.1.0.1"))
		})

		It("builds the whole spec when the config carries none of it", func() {
			out, err := applyP2PClusterConfig([]byte("apiVersion: k0s.k0sproject.io/v1beta1\nkind: ClusterConfig\n"), "10.1.0.1")
			Expect(err).ToNot(HaveOccurred())

			Expect(specAt(out, "spec", "api")).To(HaveKeyWithValue("address", "10.1.0.1"))
			Expect(specAt(out, "spec", "network", "kuberouter")).To(HaveKeyWithValue("metricsPort", 9090))
			Expect(specAt(out, "spec", "storage", "etcd")).To(HaveKeyWithValue("peerAddress", "10.1.0.1"))
		})

		It("refuses a config whose spec is not a mapping", func() {
			_, err := applyP2PClusterConfig([]byte("spec: not-a-mapping\n"), "10.1.0.1")
			Expect(err).To(MatchError(ContainSubstring("spec is not a mapping")))
		})

		It("names the full path of the key it cannot descend into", func() {
			_, err := applyP2PClusterConfig([]byte("spec:\n  network:\n    kuberouter: 8080\n"), "10.1.0.1")
			Expect(err).To(MatchError(ContainSubstring("spec.network.kuberouter is not a mapping")))
		})

		It("rejects a config that is not yaml", func() {
			_, err := applyP2PClusterConfig([]byte("\tnot yaml at all"), "10.1.0.1")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ensureK0sConfig", func() {
		var path string

		BeforeEach(func() {
			path = filepath.Join(GinkgoT().TempDir(), "k0s", "k0s.yaml")
		})

		It("leaves a config that is already there alone", func() {
			existing := "apiVersion: k0s.k0sproject.io/v1beta1\nkind: ClusterConfig\n"
			Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
			Expect(os.WriteFile(path, []byte(existing), 0600)).To(Succeed())

			// No k0s binary on the test host, so a run of `k0s config create`
			// would fail. Succeeding here is itself the assertion that the
			// user's file was not regenerated.
			Expect(ensureK0sConfig(path)).To(Succeed())

			Expect(os.ReadFile(path)).To(BeEquivalentTo(existing))
		})

		It("keeps an empty config file, rather than treating it as absent", func() {
			Expect(os.MkdirAll(filepath.Dir(path), 0755)).To(Succeed())
			Expect(os.WriteFile(path, nil, 0600)).To(Succeed())

			Expect(ensureK0sConfig(path)).To(Succeed())

			Expect(os.ReadFile(path)).To(BeEmpty())
		})

		It("reports the failure to generate one when there is no config to keep", func() {
			// Nothing wrote path, so this shells out to `k0s config create`,
			// which is not installed here.
			Expect(ensureK0sConfig(path)).To(MatchError(ContainSubstring("generating a k0s config")))
		})

		It("leaves nothing behind when the generation fails", func() {
			Expect(ensureK0sConfig(path)).ToNot(Succeed())

			// A half-written config would look like the user's on the next
			// call, and would then never be regenerated.
			Expect(path).ToNot(BeAnExistingFile())
			Expect(os.ReadDir(filepath.Dir(path))).To(BeEmpty())
		})
	})
})
