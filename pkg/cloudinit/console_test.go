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
	"errors"
	"os/exec"

	"github.com/hashicorp/go-multierror"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cloudInitConsole", Label("cloudinit", "console"), func() {
	var runner *v1mock.FakeRunner
	var logger sdkLogger.KairosLogger
	var memLog *bytes.Buffer
	var console *cloudInitConsole

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		console = newCloudInitConsole(logger, runner)
	})

	It("returns a non-nil instance and exposes the runner via getRunner", func() {
		Expect(console).ToNot(BeNil())
		Expect(console.getRunner()).To(Equal(runner))
	})

	Describe("Run", func() {
		It("returns the command output on success", func() {
			runner.ReturnValue = []byte("hello output")
			runner.ReturnError = nil

			out, err := console.Run("echo hello")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal("hello output"))
		})

		It("wraps the error with 'failed to run' on failure", func() {
			runner.ReturnValue = []byte("some output")
			runner.ReturnError = errors.New("boom")

			out, err := console.Run("false")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to run false"))
			Expect(err.Error()).To(ContainSubstring("boom"))
			// Output is still returned even on error.
			Expect(out).To(Equal("some output"))
		})

		It("applies the given options to the command", func() {
			called := false
			_, err := console.Run("echo hello", func(cmd *exec.Cmd) {
				called = true
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(called).To(BeTrue())
		})
	})

	Describe("Start", func() {
		It("runs the command and returns nil on success", func() {
			cmd := exec.Command("sh", "-c", "true")
			Expect(console.Start(cmd)).To(Succeed())
		})

		It("returns an error on command failure", func() {
			cmd := exec.Command("sh", "-c", "false")
			Expect(console.Start(cmd)).To(HaveOccurred())
		})

		It("applies the given options to the command", func() {
			called := false
			cmd := exec.Command("sh", "-c", "true")
			err := console.Start(cmd, func(c *exec.Cmd) {
				called = true
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(called).To(BeTrue())
		})
	})

	Describe("RunTemplate", func() {
		It("runs the template for each entry on success", func() {
			runner.ReturnError = nil

			err := console.RunTemplate([]string{"a", "b", "c"}, "systemctl restart %s")
			Expect(err).ToNot(HaveOccurred())
		})

		It("accumulates errors into a multierror", func() {
			runner.ReturnError = errors.New("boom")

			err := console.RunTemplate([]string{"a", "b"}, "systemctl restart %s")
			Expect(err).To(HaveOccurred())

			merr, ok := err.(*multierror.Error)
			Expect(ok).To(BeTrue())
			// One error per failing entry.
			Expect(merr.Errors).To(HaveLen(2))
			Expect(merr.Errors[0].Error()).To(ContainSubstring("failed to run systemctl restart a"))
			Expect(merr.Errors[1].Error()).To(ContainSubstring("failed to run systemctl restart b"))
		})

		It("returns nil for an empty list", func() {
			err := console.RunTemplate([]string{}, "systemctl restart %s")
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
