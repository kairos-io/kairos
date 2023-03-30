package agent_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/kairos-io/kairos/v2/internal/agent"
	"github.com/kairos-io/kairos/v2/internal/bus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const testProvider = `#!/bin/bash
event="$1"
payload=$(</dev/stdin)
echo "Received $event with $payload" >> exec.log
echo "{}"
`

var _ = Describe("Bootstrap provider", func() {
	Context("Config", func() {
		It("gets entire content", func() {
			f, err := ioutil.TempDir("", "tests")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(f)

			wd, _ := os.Getwd()
			os.WriteFile(filepath.Join(wd, "agent-provider-test"), []byte(testProvider), 0655)

			defer os.RemoveAll(filepath.Join(wd, "agent-provider-test"))

			err = os.WriteFile(filepath.Join(f, "test.config.yaml"), []byte(`#cloud-config
doo: bar`), 0655)
			Expect(err).ToNot(HaveOccurred())

			bus.Manager.Initialize()
			err = Run(WithDirectory(f))

			Expect(err).ToNot(HaveOccurred())

			dat, err := os.ReadFile(filepath.Join(wd, "exec.log"))
			Expect(err).ToNot(HaveOccurred())

			fmt.Println(string(dat))
			Expect(string(dat)).To(ContainSubstring("Received"), string(dat))
			Expect(string(dat)).To(ContainSubstring("doo: bar"), string(dat))
		})
	})
})
