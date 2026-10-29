package state

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	cnst "github.com/kairos-io/kairos/v4/immucore/internal/constants"
	internalUtils "github.com/kairos-io/kairos/v4/immucore/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rs/zerolog"
	"github.com/spectrocloud-labs/herd"
)

// kairos-io/kairos#5631: a VOLUMES entry whose mountpoint sits under another
// one was mounted before its parent on some boots, and the parent then covered
// it. The labels below exist on no disk, so each entry fails in
// resolveMountSource right after the step logs that it started on it, which is
// enough to read the order the step visits them in without mounting anything.
var _ = Describe("the custom mounts step order", func() {
	BeforeEach(func() {
		oldLogger := internalUtils.KLog.Logger
		oldStage := runMountFailureStage
		runMountFailureStage = func(string) error { return nil }
		DeferCleanup(func() {
			internalUtils.KLog.Logger = oldLogger
			runMountFailureStage = oldStage
		})
	})

	visitOrder := func(s *State) []string {
		var logbuf bytes.Buffer
		internalUtils.KLog.Logger = zerolog.New(&logbuf).Level(zerolog.DebugLevel)

		g := herd.DAG()
		Expect(g.Add(cnst.OpLoadConfig)).To(Succeed())
		Expect(s.MountCustomMountsDagStep(g)).To(Succeed())
		Expect(g.Run(context.Background())).To(Succeed())

		var order []string
		for _, line := range strings.Split(strings.TrimSpace(logbuf.String()), "\n") {
			var entry struct {
				Message string `json:"message"`
				Where   string `json:"where"`
			}
			Expect(json.Unmarshal([]byte(line), &entry)).To(Succeed(), line)
			if entry.Message == "Custom mount start" {
				order = append(order, entry.Where)
			}
		}

		return order
	}

	It("mounts a parent volume before the volume nested under it, on every run", func() {
		s := &State{
			Rootdir: GinkgoT().TempDir(),
			CustomMounts: map[string]string{
				"/dev/disk/by-label/KAIROS_NO_SUCH_CHILD":  "/usr/local/data",
				"/dev/disk/by-label/KAIROS_NO_SUCH_PARENT": "/usr/local",
			},
		}

		// Go randomises map order per range, so one passing run proves
		// nothing. The unsorted loop fails this within a few runs.
		for range 50 {
			Expect(visitOrder(s)).To(Equal([]string{"/usr/local", "/usr/local/data"}))
		}
	})
})
