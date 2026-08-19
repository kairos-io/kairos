package validation_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kairos-io/kairos-sdk/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// agentExecPathRe matches absolute-path kairos-agent invocations in bundled
// cloud-config systemd units and inittab entries (ExecStart, respawn, etc.).
var agentExecPathRe = regexp.MustCompile(`(?:ExecStart=|respawn:|systemd-inhibit )(/[^\s"']*kairos-agent)`)

var _ = Describe("Bundled cloudconfigs agent path", func() {
	It("uses constants.AgentDefaultPath for absolute kairos-agent invocations", func() {
		dir := filepath.Join("..", "bundled", "cloudconfigs")
		entries, err := os.ReadDir(dir)
		Expect(err).NotTo(HaveOccurred(), "read bundled cloudconfigs directory")

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			content, err := os.ReadFile(filepath.Join(dir, name))
			Expect(err).NotTo(HaveOccurred(), "read cloudconfig %s", name)

			for lineNum, line := range strings.Split(string(content), "\n") {
				matches := agentExecPathRe.FindAllStringSubmatch(line, -1)
				for _, match := range matches {
					Expect(match).To(HaveLen(2), "%s:%d", name, lineNum+1)
					Expect(match[1]).To(Equal(constants.AgentDefaultPath),
						"%s:%d: absolute kairos-agent path must match SDK contract", name, lineNum+1)
				}
			}
		}
	})
})
