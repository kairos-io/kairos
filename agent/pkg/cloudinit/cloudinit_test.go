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
	"os"

	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("YipCloudInitRunner", Label("cloudinit"), func() {
	var runner *v1mock.FakeRunner
	var logger sdkLogger.KairosLogger
	var memLog *bytes.Buffer
	var fs vfs.FS
	var cleanup func()
	var ci *YipCloudInitRunner

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		fs, cleanup, _ = vfst.NewTestFS(nil)
		ci = NewYipCloudInitRunner(logger, runner, fs)
	})

	AfterEach(func() {
		cleanup()
	})

	It("constructs a non-nil runner with the given fs and console", func() {
		Expect(ci).ToNot(BeNil())
		Expect(ci.fs).To(Equal(fs))
		Expect(ci.console).ToNot(BeNil())
		Expect(ci.exec).ToNot(BeNil())
	})

	Describe("Run", func() {
		It("executes the commands of a stage from a config dir", func() {
			Expect(fs.Mkdir("/conf", os.ModePerm)).To(Succeed())
			conf := `
stages:
  test:
    - commands:
      - echo hello
`
			Expect(fs.WriteFile("/conf/test.yaml", []byte(conf), os.ModePerm)).To(Succeed())

			Expect(ci.Run("test", "/conf")).To(Succeed())
			Expect(runner.CmdsMatch([][]string{{"sh", "-c", "echo hello"}})).To(Succeed())
		})

		It("fails with a source that is not a valid config", func() {
			// A non existing path is treated as a literal config string and fails to unmarshal
			Expect(ci.Run("test", "/does/not/exist")).ToNot(Succeed())
		})
	})

	Describe("SetModifier", func() {
		It("applies the modifier to loaded configs", func() {
			called := false
			ci.SetModifier(func(s []byte) ([]byte, error) {
				called = true
				return s, nil
			})

			Expect(fs.Mkdir("/conf", os.ModePerm)).To(Succeed())
			conf := `
stages:
  test:
    - commands:
      - echo hello
`
			Expect(fs.WriteFile("/conf/test.yaml", []byte(conf), os.ModePerm)).To(Succeed())
			Expect(ci.Run("test", "/conf")).To(Succeed())
			Expect(called).To(BeTrue())
		})
	})

	Describe("SetFs", func() {
		It("replaces the internal fs", func() {
			newFs, newCleanup, err := vfst.NewTestFS(nil)
			Expect(err).ToNot(HaveOccurred())
			defer newCleanup()

			ci.SetFs(newFs)
			Expect(ci.fs).To(Equal(newFs))
		})
	})

	Describe("Analyze", func() {
		It("does not panic when analyzing a stage", func() {
			Expect(func() {
				ci.Analyze("test", "/does/not/exist")
			}).ToNot(Panic())
		})
	})
})
