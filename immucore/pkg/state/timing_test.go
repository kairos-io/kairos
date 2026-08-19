package state_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/immucore/pkg/state"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spectrocloud-labs/herd"
)

var _ = Describe("step timing registry", func() {
	BeforeEach(func() {
		state.ResetTimeline()
	})

	It("records a step duration via TimedCallback run through a graph", func() {
		called := false
		g := herd.DAG(herd.EnableInit)
		Expect(g.Add("fake-op", state.TimedCallback("fake-op", func(_ context.Context) error {
			called = true
			time.Sleep(5 * time.Millisecond)
			return nil
		}))).To(Succeed())

		Expect(g.Run(context.Background())).To(Succeed())
		Expect(called).To(BeTrue())

		timings := state.Timings()
		Expect(timings).To(HaveKey("fake-op"))
		Expect(timings["fake-op"].Duration).To(BeNumerically(">=", 5*time.Millisecond))
		Expect(timings["fake-op"].Err).ToNot(HaveOccurred())
	})

	It("renders the timeline slowest-first with names and durations", func() {
		// Wide gaps so scheduling jitter on loaded CI runners cannot reorder the
		// measured durations.
		state.RunTimed("fast", func() error { time.Sleep(10 * time.Millisecond); return nil })
		state.RunTimed("slow", func() error { time.Sleep(300 * time.Millisecond); return nil })
		state.RunTimed("medium", func() error { time.Sleep(100 * time.Millisecond); return nil })

		out := state.RenderTimeline()
		Expect(out).To(ContainSubstring("<slow>"))
		Expect(out).To(ContainSubstring("<medium>"))
		Expect(out).To(ContainSubstring("<fast>"))

		// Match the step tokens, not bare words: the header text "slowest first"
		// contains "slow" and would otherwise be matched before the step.
		slowIdx := strings.Index(out, "<slow>")
		medIdx := strings.Index(out, "<medium>")
		fastIdx := strings.Index(out, "<fast>")
		Expect(slowIdx).To(BeNumerically("<", medIdx))
		Expect(medIdx).To(BeNumerically("<", fastIdx))
	})

	It("records the error status of a step", func() {
		state.RunTimed("boom", func() error { return context.Canceled })
		timings := state.Timings()
		Expect(timings).To(HaveKey("boom"))
		Expect(timings["boom"].Err).To(HaveOccurred())
	})

	It("is safe under concurrent recording", func() {
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				state.RunTimed("concurrent", func() error {
					time.Sleep(time.Millisecond)
					return nil
				})
			}()
		}
		wg.Wait()
		Expect(state.Timings()).To(HaveKey("concurrent"))
	})

	It("writes a machine-readable trace file", func() {
		state.RunTimed("traced", func() error { time.Sleep(time.Millisecond); return nil })
		dir, err := os.MkdirTemp("", "timeline")
		Expect(err).ToNot(HaveOccurred())
		defer os.RemoveAll(dir)

		path, err := state.WriteTimelineTrace(dir)
		Expect(err).ToNot(HaveOccurred())
		Expect(path).To(Equal(filepath.Join(dir, "boot-timeline.json")))

		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())

		var parsed []map[string]interface{}
		Expect(json.Unmarshal(data, &parsed)).To(Succeed())
		Expect(parsed).ToNot(BeEmpty())
		Expect(parsed[0]).To(HaveKey("name"))
		Expect(parsed[0]).To(HaveKey("duration_ms"))
	})
})
