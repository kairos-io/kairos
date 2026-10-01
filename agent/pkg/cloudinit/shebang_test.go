/*
Copyright © 2026 Kairos authors

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

package cloudinit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	realrunner "github.com/kairos-io/kairos/v4/agent/pkg/implementations/runner"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// The agent's own console runs every stage command as `sh -c <command>`, so an
// inline command whose first line is a shebang used to be read by sh, which
// treats that line as a comment and runs the body itself. Bash-only syntax was
// then dropped without an error and the stage still reported success.
// See kairos-io/kairos#2958.
var _ = Describe("a stage command that carries a shebang", Label("cloudinit", "shebang"), func() {
	var logger sdkLogger.KairosLogger
	var memLog *bytes.Buffer
	var fs vfs.FS
	var cleanup func()

	// writeStage stores a config that runs command in stage "test" and returns
	// the directory holding it.
	writeStage := func(command string) string {
		Expect(vfs.MkdirAll(fs, "/conf", 0o755)).To(Succeed())
		conf := fmt.Sprintf("stages:\n  test:\n    - commands:\n      - |\n%s\n", indent(command, 10))
		Expect(fs.WriteFile("/conf/test.yaml", []byte(conf), 0o644)).To(Succeed())

		return "/conf"
	}

	BeforeEach(func() {
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")

		var err error
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())
		// yip stores the script under the temp dir of the fs it is given.
		Expect(vfs.MkdirAll(fs, os.TempDir(), 0o777)).To(Succeed())
	})

	AfterEach(func() {
		cleanup()
	})

	Context("the command the console is handed", func() {
		var runner *v1mock.FakeRunner
		var got [][]string

		BeforeEach(func() {
			got = nil
			runner = v1mock.NewFakeRunner()
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				got = append(got, append([]string{command}, args...))
				return []byte{}, nil
			}
		})

		It("is a path to the script, not the script itself", func() {
			ci := NewYipCloudInitRunner(logger, runner, fs)
			Expect(ci.Run("test", writeStage("#!/bin/bash\necho hello"))).To(Succeed())

			Expect(got).To(HaveLen(1))
			Expect(got[0][:2]).To(Equal([]string{"sh", "-c"}))

			// The body reaching sh is what the bug looked like.
			Expect(got[0][2]).ToNot(ContainSubstring("#!/bin/bash"))
			Expect(got[0][2]).ToNot(ContainSubstring("\n"))
			Expect(filepath.Base(got[0][2])).To(HavePrefix("yip-command-"))
		})

		It("is left verbatim when there is no shebang", func() {
			ci := NewYipCloudInitRunner(logger, runner, fs)
			Expect(ci.Run("test", writeStage("echo hello"))).To(Succeed())

			Expect(got).To(HaveLen(1))
			Expect(got[0]).To(Equal([]string{"sh", "-c", "echo hello\n"}))
		})
	})

	Context("running it for real", func() {
		var ci *YipCloudInitRunner
		var marker string

		BeforeEach(func() {
			// The script writes here, so it has to be a path on the host and
			// not inside the test fs.
			dir, err := os.MkdirTemp("", "kairos-shebang-")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() { os.RemoveAll(dir) })
			marker = filepath.Join(dir, "marker")

			ci = NewYipCloudInitRunner(logger, &realrunner.RealRunner{Logger: &logger}, fs)
		})

		It("is read by the interpreter the shebang names", func() {
			script := fmt.Sprintf("#!/bin/bash\nprintf '%%s' \"${BASH_VERSION:-none}\" > %s\n", marker)
			Expect(ci.Run("test", writeStage(script))).To(Succeed())

			out, err := os.ReadFile(marker)
			Expect(err).ToNot(HaveOccurred())
			// Empty under sh, set only when bash itself read the script.
			Expect(string(out)).ToNot(Equal("none"))
			Expect(string(out)).ToNot(BeEmpty())
		})

		It("keeps bash-only syntax working, which is what #2958 reported", func() {
			// `sh -c` on a shell that is not bash drops this without an error.
			script := fmt.Sprintf("#!/bin/bash\nwords=(one two three)\nprintf '%%s' \"${words[1]}\" > %s\n", marker)
			Expect(ci.Run("test", writeStage(script))).To(Succeed())

			out, err := os.ReadFile(marker)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(Equal("two"))
		})

		It("leaves no script behind", func() {
			Expect(ci.Run("test", writeStage("#!/bin/bash\ntrue"))).To(Succeed())

			entries, err := fs.ReadDir(os.TempDir())
			Expect(err).ToNot(HaveOccurred())
			for _, e := range entries {
				Expect(e.Name()).ToNot(HavePrefix("yip-command-"))
			}
		})
	})
})

// indent prefixes every line of s with n spaces, so it can be embedded in a
// YAML block scalar.
func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}

	return strings.Join(lines, "\n")
}
