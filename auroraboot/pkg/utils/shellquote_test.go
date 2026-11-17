/*
Copyright © 2021 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils_test

import (
	"os/exec"

	"github.com/kairos-io/AuroraBoot/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// shOutput runs `sh -c "printf %s <quoted>"` so the assertion is made by a real
// shell: the point of ShellQuote is what /bin/sh does with its result, not what
// the Go string looks like.
func shOutput(s string) string {
	out, err := exec.Command("/bin/sh", "-c", "printf %s "+utils.ShellQuote(s)).CombinedOutput()
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), string(out))
	return string(out)
}

var _ = Describe("ShellQuote", func() {
	It("hands a plain command through unchanged", func() {
		Expect(shOutput("kairos-agent state")).To(Equal("kairos-agent state"))
	})

	It("keeps a redirection from being read by the calling shell", func() {
		Expect(shOutput("dmesg > /run/dmesg.log")).To(Equal("dmesg > /run/dmesg.log"))
	})

	It("keeps a glob unexpanded", func() {
		Expect(shOutput("cat /oem/* > /run/oem.yaml")).To(Equal("cat /oem/* > /run/oem.yaml"))
	})

	It("survives a single quote", func() {
		Expect(shOutput(`kairos-agent state get "kairos.eficerts|tojson"`)).
			To(Equal(`kairos-agent state get "kairos.eficerts|tojson"`))
		Expect(shOutput("echo it's here")).To(Equal("echo it's here"))
	})

	It("does not let a quoted command break out and run a second one", func() {
		// Without the '\'' escape this would close the quote and run `id`.
		Expect(shOutput("echo '; id; echo '")).To(Equal("echo '; id; echo '"))
	})

	It("quotes the empty string into an argument that still exists", func() {
		Expect(utils.ShellQuote("")).To(Equal("''"))
	})
})
