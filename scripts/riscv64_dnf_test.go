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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// riscv64DnfSteps returns the body of every RUN in the riscv64 stage of a
// Dockerfile that calls dnf, with the line continuations joined.
func riscv64DnfSteps(dockerfile string) []string {
	GinkgoHelper()
	raw, err := os.ReadFile(dockerfile)
	Expect(err).ToNot(HaveOccurred())

	var instructions []string
	var current string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if trimmed, ok := strings.CutSuffix(line, "\\"); ok {
			current += trimmed + " "
			continue
		}
		instructions = append(instructions, current+line)
		current = ""
	}

	var steps []string
	inStage := false
	for _, instruction := range instructions {
		if strings.HasPrefix(instruction, "FROM ") {
			inStage = strings.HasSuffix(strings.TrimSpace(instruction), " AS riscv64")
			continue
		}
		if body, ok := strings.CutPrefix(instruction, "RUN "); ok && inStage && strings.Contains(body, "dnf ") {
			steps = append(steps, body)
		}
	}
	Expect(steps).ToNot(BeEmpty(), "no dnf step found in the riscv64 stage of %s", dockerfile)
	return steps
}

// buildWithDnfFailingOn runs the steps one by one, as the image build does,
// with a dnf that fails whenever its arguments name pkg. It returns false as
// soon as one step fails.
func buildWithDnfFailingOn(steps []string, pkg string) bool {
	GinkgoHelper()
	bin := GinkgoT().TempDir()
	stub := "#!/bin/sh\ncase \" $* \" in *\" " + pkg + " \"*) exit 1 ;; esac\nexit 0\n"
	Expect(os.WriteFile(filepath.Join(bin, "dnf"), []byte(stub), 0o755)).To(Succeed())
	scripts, err := filepath.Abs(".")
	Expect(err).ToNot(HaveOccurred())

	for _, step := range steps {
		cmd := exec.Command("sh", "-c", step)
		cmd.Env = append(os.Environ(),
			"PATH="+bin+":"+scripts+":"+os.Getenv("PATH"),
			"RETRY_DELAY=0")
		if err := cmd.Run(); err != nil {
			return false
		}
	}
	return true
}

// Only the optional EFI install may fail in the riscv64 stage. A trailing
// || true on the whole dnf chain used to hide a failed base install, so the
// image was published without xorriso. See
// https://github.com/kairos-io/kairos/issues/5823.
var _ = Describe("riscv64 package install", func() {
	for _, dockerfile := range []string{"../Dockerfile", "../Dockerfile.riscv64"} {
		Context(dockerfile, func() {
			It("fails the build when a base package cannot be installed", func() {
				Expect(buildWithDnfFailingOn(riscv64DnfSteps(dockerfile), "xorriso")).To(BeFalse())
			})

			It("fails the build when the update fails", func() {
				Expect(buildWithDnfFailingOn(riscv64DnfSteps(dockerfile), "update")).To(BeFalse())
			})

			It("still builds when an optional EFI package cannot be installed", func() {
				Expect(buildWithDnfFailingOn(riscv64DnfSteps(dockerfile), "grub2-efi-riscv64")).To(BeTrue())
			})
		})
	}
})
