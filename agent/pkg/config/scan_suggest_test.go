package config_test

import (
	"io"
	"os"
	"strings"

	pkgConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// scanStdout runs a scan over the given cloud config with logging on, and
// returns what it wrote to stdout. The warnings are printed rather than
// logged, so reading them back is the only way to see what a user would.
func scanStdout(cloudConfig string) string {
	original := os.Stdout
	read, write, err := os.Pipe()
	Expect(err).ToNot(HaveOccurred())
	os.Stdout = write

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(read)
		done <- string(out)
	}()

	_, scanErr := pkgConfig.Scan(collector.Readers(strings.NewReader(cloudConfig)))

	os.Stdout = original
	Expect(write.Close()).To(Succeed())
	output := <-done
	Expect(read.Close()).To(Succeed())
	Expect(scanErr).ToNot(HaveOccurred())

	return output
}

var _ = Describe("Scan unknown key suggestions", func() {
	It("warns about a key that is a misspelling of one in the schema", func() {
		// `uses` is dropped on the floor: no reader looks at it, and the
		// config validates, so without this the only symptom is that the
		// user is not created.
		Expect(scanStdout(`#cloud-config
uses:
  - name: kairos
    passwd: kairos
`)).To(ContainSubstring(`WARNING: unknown configuration key "uses", which is ignored; did you mean "users"?`))
	})

	It("says nothing about keys that are outside the schema on purpose", func() {
		output := scanStdout(`#cloud-config
users:
  - name: kairos
    passwd: kairos
k3s:
  enabled: true
hostname: node-1
`)
		Expect(output).ToNot(ContainSubstring("did you mean"))
	})

	It("says nothing about a config whose keys are all in the schema", func() {
		output := scanStdout(`#cloud-config
install:
  device: /dev/sda
users:
  - name: kairos
    passwd: kairos
`)
		Expect(output).ToNot(ContainSubstring("did you mean"))
	})
})
