package state_test

import (
	"context"
	"errors"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	"github.com/kairos-io/kairos/v4/immucore/pkg/state"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

// entry finds a node in the analyzed graph by name.
func entry(g *herd.Graph, name string) herd.GraphEntry {
	for _, layer := range g.Analyze() {
		for _, e := range layer {
			if e.Name == name {
				return e
			}
		}
	}

	Fail("no graph entry named " + name)

	return herd.GraphEntry{}
}

var _ = Describe("MountCustomBindsDagStep dependencies", func() {
	var g *herd.Graph
	var order []string

	// stub stands in for one of the mount steps OpMountBind is wired to. Each
	// one records that it ran, so the specs can assert the ordering the weak
	// edges have to keep.
	stub := func(name string, err error, deps ...string) {
		opts := []herd.OpOption{herd.WithCallback(func(_ context.Context) error {
			order = append(order, name)
			return err
		})}
		if len(deps) > 0 {
			opts = append(opts, herd.WithDeps(deps...))
		}
		Expect(g.Add(name, opts...)).To(Succeed())
	}

	BeforeEach(func() {
		internalUtils.KLog = logger.NewNullLogger()
		g = herd.DAG()
		order = nil
	})

	// The bind list is empty, so the step's loop body never runs and no real
	// mount is attempted. What is under test is whether herd executes the node
	// at all, which is decided by the dependency edges alone.
	addBindStep := func() {
		s := &state.State{Rootdir: GinkgoT().TempDir()}
		Expect(s.MountCustomBindsDagStep(g)).To(Succeed())
	}

	It("still mounts the binds when one custom mount failed", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, errors.New("extra volume has no such label"), cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).To(Succeed())

		bind := entry(g, cnst.OpMountBind)
		Expect(bind.Executed).To(BeTrue(),
			"one failed extra volume must not skip every persistent bind")
		Expect(bind.Error).ToNot(HaveOccurred())
	})

	It("still mounts the binds when an overlay path failed", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, nil, cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, errors.New("overlay mount failed"), cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).To(Succeed())

		Expect(entry(g, cnst.OpMountBind).Executed).To(BeTrue())
	})

	// Weak only drops the failure cascade. The edge stays, so the binds must
	// still run after the partition they bind from has been mounted.
	It("runs after both mount steps, failed or not", func() {
		stub(cnst.OpLoadConfig, nil)
		stub(cnst.OpCustomMounts, errors.New("extra volume has no such label"), cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).To(Succeed())

		Expect(order).To(ContainElements(cnst.OpLoadConfig, cnst.OpCustomMounts, cnst.OpOverlayMount))
		Expect(order).To(HaveLen(3), "the three stubs, and only those, record into order")
		bind := entry(g, cnst.OpMountBind)
		Expect(bind.Executed).To(BeTrue())
		for _, dep := range []string{cnst.OpCustomMounts, cnst.OpOverlayMount} {
			Expect(entry(g, dep).Executed).To(BeTrue(),
				"herd must have finished %s before running the binds", dep)
		}
	})

	// OpLoadConfig is what fills State.BindMounts, so it stays a hard
	// dependency: with no list there is nothing to bind and running anyway
	// would only hide the real failure.
	It("does not mount the binds when the layout config could not be read", func() {
		stub(cnst.OpLoadConfig, errors.New("cannot read /run/cos/cos-layout.env"))
		stub(cnst.OpCustomMounts, nil, cnst.OpLoadConfig)
		stub(cnst.OpOverlayMount, nil, cnst.OpLoadConfig)
		addBindStep()

		Expect(g.Run(context.Background())).To(Succeed())

		Expect(entry(g, cnst.OpMountBind).Executed).To(BeFalse())
	})
})
