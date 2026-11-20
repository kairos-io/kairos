package auroraboot

import (
	"bytes"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sdklogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/rs/zerolog"
)

var _ = Describe("teeKairosLogger", func() {
	It("routes an event emitted through the returned logger to sink", func() {
		var sink bytes.Buffer
		base := sdklogger.NewNullLogger()
		tee := teeKairosLogger(base, &sink)
		tee.Logger.Info().Str("k", "v").Msg("progress line")
		Expect(sink.String()).To(ContainSubstring("progress line"))
		Expect(sink.String()).To(ContainSubstring("k=v"))
	})

	It("does not touch the base logger's own output", func() {
		// A NullLogger discards its own events; teeing must build a fresh
		// zerolog Logger for the returned KairosLogger rather than mutating
		// base. Emitting through base after tee is constructed must land
		// nowhere - in particular, not in sink.
		var sink bytes.Buffer
		base := sdklogger.NewNullLogger()
		_ = teeKairosLogger(base, &sink)
		base.Logger.Info().Msg("via base only")
		Expect(sink.Len()).To(Equal(0))
	})

	It("keeps debug events out of sink when the base logger runs at debug", func() {
		// Debug events can carry whole documents, such as the deployer's
		// cloud-config, and sink is the build log every UI client reads.
		var sink bytes.Buffer
		base := sdklogger.KairosLogger{Logger: zerolog.New(io.Discard).Level(zerolog.DebugLevel)}
		tee := teeKairosLogger(base, &sink)
		tee.Logger.Debug().Str("cloudConfig", "marker-secret").Msg("Cloud config contents")
		tee.Logger.Info().Msg("Copying cloud config")
		Expect(sink.String()).To(ContainSubstring("Copying cloud config"))
		Expect(sink.String()).NotTo(ContainSubstring("marker-secret"))
	})
})
