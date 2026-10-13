package webui

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("stripANSI", func() {
	// The line the QA run captured on the progress websocket, byte for byte:
	// zerolog's console writer colours the timestamp, the level, the message
	// and each field key separately.
	const agentLine = "\x1b[90m2026-09-16T15:21:51Z\x1b[0m \x1b[32mINF\x1b[0m " +
		"\x1b[1mKairos Agent\x1b[0m \x1b[36mversion=\x1b[0mv0.0.0-qa4585"

	It("renders the agent's log line as the text it was meant to be", func() {
		Expect(stripANSI(agentLine)).To(Equal(
			"2026-09-16T15:21:51Z INF Kairos Agent version=v0.0.0-qa4585"))
	})

	It("leaves no escape byte anywhere in the result", func() {
		Expect(stripANSI(agentLine)).NotTo(ContainSubstring("\x1b"))
		// Pinned so the assertion cannot pass by the codes never having been
		// there: the input really does carry eight of them.
		Expect(strings.Count(agentLine, "\x1b")).To(Equal(8))
	})

	It("empties a line that was only a colour reset", func() {
		// 61 of the 172 frames in one install were exactly this. They are what
		// made the log pane a third blank rows.
		Expect(stripANSI("\x1b[0m")).To(BeEmpty())
	})

	It("passes a line with no escape codes through untouched", func() {
		Expect(stripANSI("partitioning /dev/vda")).To(Equal("partitioning /dev/vda"))
	})

	It("keeps leading whitespace, which a transcript reads as structure", func() {
		Expect(stripANSI("\x1b[32m    indented\x1b[0m")).To(Equal("    indented"))
	})

	It("strips cursor and erase sequences, not just colours", func() {
		Expect(stripANSI("pulling\x1b[2K\x1b[1Gdone")).To(Equal("pullingdone"))
	})

	It("does not eat text that merely looks like a sequence", func() {
		Expect(stripANSI("see [0m in the docs")).To(Equal("see [0m in the docs"))
	})
})
