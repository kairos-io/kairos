package progress_test

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-agent/v2/pkg/progress"
	"github.com/kairos-io/kairos-sdk/agentrun"
)

var _ = Describe("Progress events", func() {
	var buf *bytes.Buffer

	BeforeEach(func() {
		buf = &bytes.Buffer{}
		old := progress.Output
		progress.Output = buf
		DeferCleanup(func() { progress.Output = old })
	})

	Context("when emission is enabled", func() {
		BeforeEach(func() {
			GinkgoT().Setenv(agentrun.EnvProgress, "1")
		})

		It("writes a step event as a JSON line", func() {
			progress.EmitStep(agentrun.StepPartition)
			Expect(buf.String()).To(MatchJSON(`{"event":"step","step":"partition"}`))
		})

		It("writes an error event as a JSON line", func() {
			progress.EmitError("disk exploded")
			Expect(buf.String()).To(MatchJSON(`{"event":"error","message":"disk exploded"}`))
		})

		It("preserves newlines in error messages (escaped by JSON)", func() {
			progress.EmitError("line one\nline two")
			Expect(buf.String()).To(MatchJSON(`{"event":"error","message":"line one\nline two"}`))
		})

		It("emits exactly one newline-terminated line per event", func() {
			progress.EmitStep(agentrun.StepDone)
			Expect(buf.String()).To(HaveSuffix("\n"))
			Expect(bytes.Count(buf.Bytes(), []byte("\n"))).To(Equal(1))
		})
	})

	Context("when emission is disabled", func() {
		BeforeEach(func() {
			GinkgoT().Setenv(agentrun.EnvProgress, "")
		})

		It("writes nothing", func() {
			progress.EmitStep(agentrun.StepDone)
			progress.EmitError("ignored")
			Expect(buf.Len()).To(BeZero())
		})
	})
})
