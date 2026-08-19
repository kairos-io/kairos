package state_test

import (
	"github.com/kairos-io/immucore/pkg/op"
	"github.com/kairos-io/immucore/pkg/state"
	"time"

	cnst "github.com/kairos-io/immucore/internal/constants"
	"github.com/kairos-io/immucore/pkg/dag"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("mounting immutable setup", func() {
	var g *herd.Graph

	BeforeEach(func() {
		g = herd.DAG(herd.EnableInit)
		Expect(g).ToNot(BeNil())
	})

	Context("SortedBindMounts()", func() {
		It("returns the nodes with less depth first and in alfabetical order", func() {
			s := &state.State{
				BindMounts: []string{
					"/etc/nginx/config.d/",
					"/etc/nginx",
					"/etc/kubernetes/child",
					"/etc/kubernetes",
					"/etc/kubernetes/child/grand-child",
				},
			}
			Expect(s.SortedBindMounts()).To(Equal([]string{
				"/etc/kubernetes",
				"/etc/nginx",
				"/etc/kubernetes/child",
				"/etc/nginx/config.d/",
				"/etc/kubernetes/child/grand-child",
			}))
		})
	})

	Context("simple invocation", func() {
		It("generates normal dag", func() {
			Skip("Cant override bootstate yet")
			s := &state.State{
				Rootdir:      "/",
				TargetImage:  "/cOS/myimage.img",
				TargetDevice: "/dev/disk/by-label/COS_LABEL",
			}

			err := dag.RegisterNormalBoot(s, g)
			Expect(err).ToNot(HaveOccurred())

			dag := g.Analyze()

			checkDag(dag, s.WriteDAG(g))

		})
		It("generates normal dag with extra dirs", func() {
			Skip("Cant override bootstate yet")
			s := &state.State{Rootdir: "/",
				OverlayDirs:  []string{"/etc"},
				BindMounts:   []string{"/etc/kubernetes"},
				CustomMounts: map[string]string{"COS_PERSISTENT": "/usr/local"}}

			err := dag.RegisterNormalBoot(s, g)
			Expect(err).ToNot(HaveOccurred())

			dag := g.Analyze()

			checkDag(dag, s.WriteDAG(g))
		})
		It("generates livecd dag", func() {
			s := &state.State{}
			err := dag.RegisterLiveMedia(s, g)
			Expect(err).ToNot(HaveOccurred())
			dag := g.Analyze()
			checkLiveCDDag(dag, s.WriteDAG(g))

		})

		It("generates in-RAM dag", func() {
			s := &state.State{Rootdir: "/sysroot", InRAM: true}
			err := dag.RegisterInRAMBoot(s, g)
			Expect(err).ToNot(HaveOccurred())
			checkInRAMDag(g.Analyze(), s.WriteDAG(g))
		})

		It("generates UKI dag without ensure-partitions", func() {
			s := &state.State{Rootdir: "/"}
			err := dag.RegisterUKI(s, g)
			Expect(err).ToNot(HaveOccurred())
			Expect(layerOf(g.Analyze(), cnst.OpEnsurePartitions)).To(Equal(-1), s.WriteDAG(g))
		})

		It("generates UKI in-RAM dag with ensure-partitions gating unlock and OEM", func() {
			s := &state.State{Rootdir: "/", InRAM: true}
			err := dag.RegisterUKI(s, g)
			Expect(err).ToNot(HaveOccurred())
			layers := g.Analyze()
			actualDag := s.WriteDAG(g)

			ensure := layerOf(layers, cnst.OpEnsurePartitions)
			udev := layerOf(layers, cnst.OpUkiUdev)
			unlock := layerOf(layers, cnst.OpUkiKcrypt)
			oem := layerOf(layers, cnst.OpMountOEM)

			// devices must be discovered before we scan/create partitions,
			// partitions must exist before unlock, unlock before OEM mount.
			Expect(ensure).To(BeNumerically(">", udev), actualDag)
			Expect(unlock).To(BeNumerically(">", ensure), actualDag)
			Expect(oem).To(BeNumerically(">", unlock), actualDag)
		})

		It("Mountop timeouts", func() {
			_, err := op.MountOPWithFstab("/dev/doesntexist", "/tmp/jojobizarreadventure", "", []string{}, 500*time.Millisecond)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("exhausted"))
		})
	})
})

// layerOf returns the index of the DAG layer containing the named op, or -1
// when the op is not registered at all.
func layerOf(dag [][]herd.GraphEntry, name string) int {
	for i, layer := range dag {
		for _, e := range layer {
			if e.Name == name {
				return i
			}
		}
	}
	return -1
}

func checkLiveCDDag(dag [][]herd.GraphEntry, actualDag string) {
	Expect(len(dag)).To(Equal(5), actualDag)
	Expect(len(dag[0])).To(Equal(1), actualDag)
	Expect(len(dag[1])).To(Equal(2), actualDag)
	Expect(len(dag[2])).To(Equal(1), actualDag)
	Expect(len(dag[3])).To(Equal(1), actualDag)
	Expect(len(dag[4])).To(Equal(1), actualDag)

	Expect(dag[0][0].Name).To(Equal("init"))
	Expect(dag[1][0].Name).To(Or(Equal(cnst.OpSentinel), Equal(cnst.OpWaitForSysroot)), actualDag)
	Expect(dag[1][1].Name).To(Or(Equal(cnst.OpSentinel), Equal(cnst.OpWaitForSysroot)), actualDag)
	Expect(dag[2][0].Name).To(Equal(cnst.OpMountOEM), actualDag)
	Expect(dag[3][0].Name).To(Equal(cnst.OpRootfsHook), actualDag)
	Expect(dag[4][0].Name).To(Equal(cnst.OpInitramfsHook), actualDag)

}
func checkDag(dag [][]herd.GraphEntry, actualDag string) {
	Expect(len(dag)).To(Equal(12), actualDag)
	Expect(len(dag[0])).To(Equal(1), actualDag)
	Expect(len(dag[1])).To(Equal(4), actualDag)
	Expect(len(dag[2])).To(Equal(1), actualDag)
	Expect(len(dag[3])).To(Equal(1), actualDag)
	Expect(len(dag[4])).To(Equal(1), actualDag)
	Expect(len(dag[5])).To(Equal(1), actualDag)
	Expect(len(dag[6])).To(Equal(1), actualDag)
	Expect(len(dag[7])).To(Equal(2), actualDag)
	Expect(len(dag[8])).To(Equal(1), actualDag)
	Expect(len(dag[9])).To(Equal(1), actualDag)
	Expect(len(dag[10])).To(Equal(1), actualDag)
	Expect(len(dag[11])).To(Equal(1), actualDag)

	Expect(dag[0][0].Name).To(Equal("init"))
	Expect(dag[1][0].Name).To(Or(
		Equal(cnst.OpMountTmpfs),
		Equal(cnst.OpSentinel),
		Equal(cnst.OpMountState),
		Equal(cnst.OpLvmActivate),
	), actualDag)
	Expect(dag[1][1].Name).To(Or(
		Equal(cnst.OpMountTmpfs),
		Equal(cnst.OpSentinel),
		Equal(cnst.OpMountState),
		Equal(cnst.OpLvmActivate),
	), actualDag)
	Expect(dag[1][2].Name).To(Or(
		Equal(cnst.OpMountTmpfs),
		Equal(cnst.OpSentinel),
		Equal(cnst.OpMountState),
		Equal(cnst.OpLvmActivate),
	), actualDag)
	Expect(dag[1][3].Name).To(Or(
		Equal(cnst.OpMountTmpfs),
		Equal(cnst.OpSentinel),
		Equal(cnst.OpMountState),
		Equal(cnst.OpLvmActivate),
	), actualDag)
	Expect(dag[2][0].Name).To(Equal(cnst.OpDiscoverState), actualDag)
	Expect(dag[3][0].Name).To(Equal(cnst.OpMountRoot), actualDag)
	Expect(dag[4][0].Name).To(Equal(cnst.OpMountOEM), actualDag)
	Expect(dag[5][0].Name).To(Equal(cnst.OpRootfsHook), actualDag)
	Expect(dag[6][0].Name).To(Equal(cnst.OpLoadConfig), actualDag)
	Expect(dag[7][0].Name).To(Or(Equal(cnst.OpMountBaseOverlay), Equal(cnst.OpCustomMounts)), actualDag)
	Expect(dag[7][1].Name).To(Or(Equal(cnst.OpMountBaseOverlay), Equal(cnst.OpCustomMounts)), actualDag)
	Expect(dag[8][0].Name).To(Equal(cnst.OpOverlayMount), actualDag)
	Expect(dag[9][0].Name).To(Equal(cnst.OpMountBind), actualDag)
	Expect(dag[10][0].Name).To(Equal(cnst.OpWriteFstab), actualDag)
	Expect(dag[11][0].Name).To(Equal(cnst.OpInitramfsHook), actualDag)
}

// checkInRAMDag asserts the shape of the DAG produced by RegisterInRAMBoot.
// It mirrors the normal-boot DAG minus LVM activation, mount-state,
// discover-state, and mount-root — dracut's rd.live.ram provides /sysroot for
// us, so we skip straight to waiting for it. Every other step (kcrypt, oem,
// rootfs, load-config, mounts, overlays, binds, fstab, initramfs) is reused
// verbatim from the shared step set. ensure-partitions is inserted between
// wait-for-sysroot and every step that expects Kairos partition labels to
// exist, so first-boot workstations get their COS_OEM/COS_PERSISTENT created
// (when the auto-create flag is set) before mount-oem or kcrypt fire.
func checkInRAMDag(dag [][]herd.GraphEntry, actualDag string) {
	// Names by op-constant present at each layer. Order within a layer is not
	// guaranteed, so we assert as a set.
	expected := [][]string{
		{"init"},
		{cnst.OpSentinel, cnst.OpWaitForSysroot, cnst.OpMountTmpfs},
		{cnst.OpEnsurePartitions},
		{cnst.OpKcryptUpgrade, cnst.OpMountOEM},
		{cnst.OpKcryptUnlock, cnst.OpRootfsHook},
		{cnst.OpLoadConfig},
		{cnst.OpMountBaseOverlay, cnst.OpCustomMounts},
		{cnst.OpOverlayMount},
		{cnst.OpMountBind},
		{cnst.OpWriteFstab, cnst.OpUkiCopySysExtensions},
		{cnst.OpInitramfsHook},
	}
	Expect(len(dag)).To(Equal(len(expected)), actualDag)
	for i, want := range expected {
		got := make([]string, 0, len(dag[i]))
		for _, e := range dag[i] {
			got = append(got, e.Name)
		}
		Expect(got).To(ConsistOf(want), actualDag)
	}

	// Explicitly verify the normal-boot-only steps are absent — that is the
	// whole point of the in-RAM DAG.
	all := map[string]bool{}
	for _, layer := range dag {
		for _, e := range layer {
			all[e.Name] = true
		}
	}
	for _, absent := range []string{cnst.OpMountRoot, cnst.OpDiscoverState, cnst.OpMountState, cnst.OpLvmActivate} {
		Expect(all[absent]).To(BeFalse(), "expected %s to be absent from in-RAM DAG\n%s", absent, actualDag)
	}
}
