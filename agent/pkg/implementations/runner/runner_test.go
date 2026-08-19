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

package runner_test

import (
	"bytes"

	v1 "github.com/kairos-io/kairos-agent/v2/pkg/implementations/runner"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	"github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Runner", Label("types", "runner"), func() {
	It("Runs commands on the real Runner", func() {
		r := v1.RealRunner{}
		_, err := r.Run("pwd")
		Expect(err).To(BeNil())
	})
	It("Runs commands on the fake runner", func() {
		r := v1mock.NewFakeRunner()
		_, err := r.Run("pwd")
		Expect(err).To(BeNil())
	})
	It("Sets and gets the logger on the fake runner", func() {
		r := v1mock.NewFakeRunner()
		Expect(r.GetLogger()).To(BeNil())
		logger := logger.NewNullLogger()
		r.SetLogger(&logger)
		Expect(r.GetLogger()).ToNot(BeNil())
	})
	It("Sets and gets the logger on the real runner", func() {
		r := v1.RealRunner{}
		Expect(r.GetLogger()).To(BeNil())
		logger := logger.NewNullLogger()
		r.SetLogger(&logger)
		Expect(r.GetLogger()).ToNot(BeNil())
	})

	It("logs the command when on debug", func() {

		memLog := &bytes.Buffer{}
		logger := logger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		r := v1.RealRunner{Logger: &logger}
		_, err := r.Run("command", "with", "args")
		Expect(err).ToNot(BeNil()) // Command will fail
		Expect(memLog.String()).To(ContainSubstring("command with args"))
	})
})
