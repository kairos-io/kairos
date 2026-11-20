/*
Copyright © 2026 The Kairos Authors

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

package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// scripts/retry wraps the network fetches in the Dockerfiles, so a dropped
// request no longer reddens the whole image build. See
// https://github.com/kairos-io/kairos/issues/5565.
var _ = Describe("scripts/retry", func() {
	var counter string

	// run invokes the helper with RETRY_DELAY=0 so the specs do not sleep,
	// and returns its exit status together with everything it printed.
	run := func(env []string, args ...string) (int, string) {
		GinkgoHelper()
		cmd := exec.Command("./retry", args...)
		cmd.Env = append(append(os.Environ(), "RETRY_DELAY=0"), env...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return 0, string(out)
		}
		var exitErr *exec.ExitError
		Expect(err).To(BeAssignableToTypeOf(exitErr), "retry failed to start: %v", err)
		return err.(*exec.ExitError).ExitCode(), string(out)
	}

	// failUntil returns a shell command that counts its own invocations in a
	// file and only succeeds once it has been called n times.
	failUntil := func(n int) []string {
		return []string{"sh", "-c",
			`n=$(cat "$0" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "$0"; [ "$n" -ge ` +
				strconv.Itoa(n) + ` ]`, counter}
	}

	attempts := func() int {
		GinkgoHelper()
		raw, err := os.ReadFile(counter)
		Expect(err).ToNot(HaveOccurred())
		n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		Expect(err).ToNot(HaveOccurred())
		return n
	}

	BeforeEach(func() {
		counter = filepath.Join(GinkgoT().TempDir(), "attempts")
	})

	It("runs the command once when it succeeds", func() {
		status, _ := run(nil, failUntil(1)...)
		Expect(status).To(Equal(0))
		Expect(attempts()).To(Equal(1))
	})

	It("passes every argument through untouched", func() {
		status, out := run(nil, "printf", "%s-%s\n", "first", "second")
		Expect(status).To(Equal(0))
		Expect(out).To(Equal("first-second\n"))
	})

	It("tries again until the command succeeds", func() {
		status, out := run(nil, failUntil(3)...)
		Expect(status).To(Equal(0))
		Expect(attempts()).To(Equal(3))
		Expect(out).To(ContainSubstring("attempt 1 of 3"))
		Expect(out).To(ContainSubstring("attempt 2 of 3"))
	})

	It("gives up after the last attempt and keeps the command's exit status", func() {
		status, out := run(nil, "sh", "-c", `echo x >> "$0"; exit 7`, counter)
		Expect(status).To(Equal(7), "the caller must still see why the command failed")
		Expect(out).To(ContainSubstring("giving up"))

		raw, err := os.ReadFile(counter)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Count(string(raw), "x")).To(Equal(3))
	})

	It("honours RETRY_ATTEMPTS", func() {
		status, _ := run([]string{"RETRY_ATTEMPTS=5"}, failUntil(5)...)
		Expect(status).To(Equal(0))
		Expect(attempts()).To(Equal(5))
	})

	It("refuses to run with no command", func() {
		status, out := run(nil)
		Expect(status).To(Equal(2))
		Expect(out).To(ContainSubstring("no command given"))
	})
})
