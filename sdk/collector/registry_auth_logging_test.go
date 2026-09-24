package collector_test

import (
	"bytes"
	"io"
	"os"
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("reader parse diagnostics", func() {
	It("does not print malformed reader contents", func() {
		const secret = "registry-password-sentinel-2613"
		reader := strings.NewReader("registry-auth: [" + secret)
		o := &Options{}
		Expect(o.Apply(Readers(reader))).To(Succeed())

		originalStdout := os.Stdout
		defer func() { os.Stdout = originalStdout }()
		stdoutReader, stdoutWriter, err := os.Pipe()
		Expect(err).ToNot(HaveOccurred())
		os.Stdout = stdoutWriter
		_, scanErr := Scan(o, func(data []byte) ([]byte, error) { return data, nil })
		Expect(stdoutWriter.Close()).To(Succeed())
		output, err := io.ReadAll(stdoutReader)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdoutReader.Close()).To(Succeed())

		Expect(scanErr).ToNot(HaveOccurred())
		Expect(string(output)).To(ContainSubstring("Error unmarshalling config:"))
		Expect(string(output)).ToNot(ContainSubstring(secret))
		Expect(bytes.TrimSpace(output)).ToNot(BeEmpty())
	})
})
