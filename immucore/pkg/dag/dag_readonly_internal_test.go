package dag

import (
	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("the read-only media wiring", func() {
	// Naming an op that was never registered does not fail: herd hands the name
	// to depgraph.DependOn, which creates the node instead of complaining, and
	// the node then has no entry in the ops map. The first Analyze() locks every
	// node it walks, so the nil *OpState dereferences and immucore panics before
	// it has mounted anything. These specs pin that the writable path never
	// names the snapshot op.

	It("adds no dependency on a writable install", func() {
		g := herd.DAG(herd.EnableInit)
		s := &state.State{}

		Expect(g.Add(cnst.OpCustomMounts, s.WriteProtectedSnapshotDeps()...)).To(Succeed())
		Expect(func() { g.Analyze() }).ToNot(Panic(),
			"the dependency named an op that was never registered")

		for _, layer := range g.Analyze() {
			for _, op := range layer {
				Expect(op.Name).ToNot(Equal(cnst.OpPersistentSnapshot))
			}
		}
	})

	It("mounts the custom volumes after the snapshot when the media is write-protected", func() {
		g := herd.DAG(herd.EnableInit)
		s := &state.State{WriteProtected: true}

		// Registered first, because that is the contract: the dependency may
		// only be used where the step itself was also registered.
		Expect(g.Add(cnst.OpPersistentSnapshot)).To(Succeed())
		Expect(g.Add(cnst.OpCustomMounts, s.WriteProtectedSnapshotDeps()...)).To(Succeed())
		Expect(func() { g.Analyze() }).ToNot(Panic())

		Expect(layerIndexOf(g, cnst.OpPersistentSnapshot)).
			To(BeNumerically("<", layerIndexOf(g, cnst.OpCustomMounts)),
				"the snapshot has to exist before the custom mounts try to mount it")
	})
})

// layerIndexOf returns which layer of the analyzed graph name landed in, or -1.
func layerIndexOf(g *herd.Graph, name string) int {
	for i, layer := range g.Analyze() {
		for _, op := range layer {
			if op.Name == name {
				return i
			}
		}
	}
	return -1
}
