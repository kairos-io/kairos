package dag

import (
	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("ReadOnlyOverlayWeakDep", func() {
	// Naming an op that was never registered does not fail: herd hands the name
	// to depgraph.DependOn, which creates the node instead of complaining, and
	// the node then has no entry in the ops map. The first Analyze() locks every
	// node it walks, so the nil *OpState dereferences and immucore panics before
	// it has mounted anything. These specs pin that the writable path never
	// names the op.

	It("adds no dependency on a writable install", func() {
		g := herd.DAG(herd.EnableInit)
		s := &state.State{}

		Expect(g.Add("some-op", s.ReadOnlyOverlayWeakDep())).To(Succeed())
		Expect(func() { g.Analyze() }).ToNot(Panic(),
			"the weak dep named an op that was never registered")

		for _, layer := range g.Analyze() {
			for _, op := range layer {
				Expect(op.Name).ToNot(Equal(cnst.OpPersistentROOverlay))
			}
		}
	})

	It("depends on the overlay step when the media is write-protected", func() {
		g := herd.DAG(herd.EnableInit)
		s := &state.State{HardwareRO: true}

		// Registered first, because that is the contract: the option may only be
		// used where the step itself was also registered.
		Expect(g.Add(cnst.OpPersistentROOverlay)).To(Succeed())
		Expect(g.Add("some-op", s.ReadOnlyOverlayWeakDep())).To(Succeed())
		Expect(func() { g.Analyze() }).ToNot(Panic())

		Expect(layerIndexOf(g, cnst.OpPersistentROOverlay)).
			To(BeNumerically("<", layerIndexOf(g, "some-op")),
				"the overlay has to be in place before anything that depends on it")
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
