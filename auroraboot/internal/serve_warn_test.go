package internal

import (
	"bytes"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WarnUnauthenticatedServe", func() {
	var buf *bytes.Buffer
	var saved logger.KairosLogger

	BeforeEach(func() {
		buf = &bytes.Buffer{}
		saved = Log
		Log = logger.NewBufferLogger(buf)
	})
	AfterEach(func() {
		Log = saved
	})

	It("warns about the missing authentication, naming the address and server", func() {
		WarnUnauthenticatedServe("0.0.0.0:8080", "artifact HTTP server")

		out := buf.String()
		Expect(out).To(ContainSubstring("0.0.0.0:8080"))
		Expect(out).To(ContainSubstring("artifact HTTP server"))
		Expect(out).To(ContainSubstring("UNAUTHENTICATED"))
		// The boundary the warning points operators at is network isolation.
		Expect(out).To(ContainSubstring("trusted, isolated provisioning network"))
	})
})
