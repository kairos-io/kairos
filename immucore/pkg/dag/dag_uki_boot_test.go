package dag_test

import (
	"testing"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/dag"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dag test Suite")
}

// entries flattens the analyzed graph into a lookup by step name.
func entries(g *herd.Graph) map[string]herd.GraphEntry {
	out := map[string]herd.GraphEntry{}
	for _, layer := range g.Analyze() {
		for _, e := range layer {
			out[e.Name] = e
		}
	}
	return out
}

var _ = Describe("the UKI boot dag", func() {
	var steps map[string]herd.GraphEntry

	BeforeEach(func() {
		internalUtils.KLog = logger.NewKairosLogger("immucore-test", "error", true)
		g := herd.DAG(herd.EnableInit)
		Expect(dag.RegisterUKI(&state.State{Rootdir: "/sysroot"}, g)).To(Succeed())
		steps = entries(g)
	})

	It("registers the network step", func() {
		// The step used to be commented out of the graph entirely, which is
		// why remote KMS never worked in UKI mode.
		Expect(steps).To(HaveKey(cnst.OpUkiNetwork))
		Expect(steps[cnst.OpUkiNetwork].WithCallback).To(BeTrue())
	})

	It("runs the network step before the unlock", func() {
		Expect(steps).To(HaveKey(cnst.OpUkiKcrypt))
		Expect(steps[cnst.OpUkiKcrypt].Dependencies).To(ContainElement(cnst.OpUkiNetwork))
	})

	It("keeps the unlock running when the network step fails", func() {
		// A weak dependency orders the two without letting a failed network
		// setup skip the unlock: the operator has to see the KMS error, not a
		// step that was skipped before it.
		Expect(steps[cnst.OpUkiKcrypt].WeakDependencies).To(ContainElement(cnst.OpUkiNetwork))
	})

	It("still requires the steps the unlock genuinely cannot run without", func() {
		Expect(steps[cnst.OpUkiKcrypt].Dependencies).To(ContainElements(cnst.OpSentinel, cnst.OpUkiUdev))
		Expect(steps[cnst.OpUkiKcrypt].WeakDependencies).NotTo(ContainElement(cnst.OpUkiUdev))
	})

	It("sets the network step up after udev has found the interfaces", func() {
		Expect(steps[cnst.OpUkiNetwork].Dependencies).To(ContainElements(
			cnst.OpUkiBaseMounts, cnst.OpUkiKernelModules, cnst.OpUkiUdev))
	})

	It("waits for the in-RAM partitions before unlocking, when that workflow is on", func() {
		internalUtils.KLog = logger.NewKairosLogger("immucore-test", "error", true)
		g := herd.DAG(herd.EnableInit)
		Expect(dag.RegisterUKI(&state.State{Rootdir: "/sysroot", InRAM: true}, g)).To(Succeed())
		inRAM := entries(g)
		Expect(inRAM[cnst.OpUkiKcrypt].Dependencies).To(ContainElements(
			cnst.OpEnsurePartitions, cnst.OpUkiNetwork))
		Expect(inRAM[cnst.OpUkiKcrypt].WeakDependencies).NotTo(ContainElement(cnst.OpEnsurePartitions))
	})
})
