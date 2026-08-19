/*
Copyright © 2022 SUSE LLC

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
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/hashicorp/go-multierror"

	"github.com/kairos-io/kairos-agent/v2/pkg/cloudinit"
	agentConfig "github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils"
	"github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkFs "github.com/kairos-io/kairos-sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

func writeCmdline(s string, fs sdkFs.KairosFS) error {
	if err := fs.Mkdir("/proc", os.ModePerm); err != nil {
		return err
	}
	return fs.WriteFile("/proc/cmdline", []byte(s), os.ModePerm)
}

// multiErrorCIRunner fails every stage with a multierror that does not wrap
// yaml type errors, so it is not absorbed as a partial unmarshal failure.
type multiErrorCIRunner struct {
	v1mock.FakeCloudInitRunner
}

func (c *multiErrorCIRunner) Run(stage string, args ...string) error {
	return multierror.Append(nil, errors.New("generic cloud init failure"))
}

// argsRecordingCIRunner captures every args tuple passed to Run so specs can
// assert on the values reaching the runner (in particular that a templated
// cmdline URI was rendered before handoff).
type argsRecordingCIRunner struct {
	v1mock.FakeCloudInitRunner
	Args [][]string
}

func (c *argsRecordingCIRunner) Run(stage string, args ...string) error {
	c.ExecStages = append(c.ExecStages, stage)
	c.Args = append(c.Args, append([]string{}, args...))
	return nil
}

var _ = Describe("run stage", Label("RunStage"), func() {
	var config *sdkConfig.Config
	var runner *v1mock.FakeRunner
	var logger sdkLogger.KairosLogger
	var syscall *v1mock.FakeSyscall
	var client *v1mock.FakeHTTPClient
	var mounter *v1mock.ErrorMounter
	var fs vfs.FS
	var memLog *bytes.Buffer

	var cleanup func()

	BeforeEach(func() {
		runner = v1mock.NewFakeRunner()
		// Use a different config with a buffer for logger, so we can check the output
		// We also use the real fs
		memLog = &bytes.Buffer{}
		logger = sdkLogger.NewBufferLogger(memLog)
		logger.SetLevel("debug")
		fs, cleanup, _ = vfst.NewTestFS(nil)

		config = agentConfig.NewConfig(
			agentConfig.WithFs(fs),
			agentConfig.WithRunner(runner),
			agentConfig.WithLogger(logger),
			agentConfig.WithMounter(mounter),
			agentConfig.WithSyscall(syscall),
			agentConfig.WithClient(client),
		)

		config.CloudInitRunner = cloudinit.NewYipCloudInitRunner(config.Logger, config.Runner, fs)
	})
	AfterEach(func() { cleanup() })

	It("fails if strict mode is enabled", Label("strict"), func() {
		d, err := fsutils.TempDir(fs, "", "elemental")
		Expect(err).ToNot(HaveOccurred())
		_ = fs.WriteFile(fmt.Sprintf("%s/test.yaml", d), []byte("stages: [foo,bar]"), os.ModePerm)
		config.Strict = true
		Expect(utils.RunStage(config, "c3po")).ToNot(BeNil())
	})

	It("does not fail but prints errors by default", Label("strict"), func() {
		writeCmdline("stages.c3po[0].datasource", fs)

		config.Logger.SetLevel("debug")
		out := utils.RunStage(config, "c3po")
		Expect(out).To(BeNil())
		Expect(memLog.String()).To(ContainSubstring("parsing returned errors"))
	})

	It("Goes over extra paths", func() {
		d, err := fsutils.TempDir(fs, "", "elemental")
		Expect(err).ToNot(HaveOccurred())
		config.Logger.SetLevel("debug")
		config.CloudInitPaths = []string{d}

		Expect(utils.RunStage(config, "luke")).To(BeNil())
		Expect(memLog.String()).To(ContainSubstring(d))
		Expect(memLog).To(ContainSubstring("luke"))
		Expect(memLog).To(ContainSubstring("luke.before"))
		Expect(memLog).To(ContainSubstring("luke.after"))
	})

	It("parses cmdline uri", func() {
		d, _ := fsutils.TempDir(fs, "", "elemental")
		_ = fs.WriteFile(fmt.Sprintf("%s/test.yaml", d), []byte{}, os.ModePerm)

		writeCmdline(fmt.Sprintf("cos.setup=%s/test.yaml", d), fs)

		Expect(utils.RunStage(config, "padme")).To(BeNil())
		Expect(memLog).To(ContainSubstring("padme"))
		Expect(memLog).To(ContainSubstring(fmt.Sprintf("%s/test.yaml", d)))
	})

	It("parses cmdline uri from kairos.config_url=", func() {
		// Modern replacement for the legacy cos.setup= stanza. Both must
		// resolve through the same SDK-backed helper (kairos-sdk PR #812),
		// so the RunStage side effect (yip source added) is identical.
		d, err := fsutils.TempDir(fs, "", "elemental")
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile(fmt.Sprintf("%s/test.yaml", d), []byte{}, os.ModePerm)
		Expect(err).ToNot(HaveOccurred())

		Expect(writeCmdline(fmt.Sprintf("root=LABEL=X kairos.config_url=%s/test.yaml quiet", d), fs)).To(Succeed())
		Expect(utils.RunStage(config, "padme")).To(BeNil())
		Expect(memLog).To(ContainSubstring("padme"))
		Expect(memLog).To(ContainSubstring(fmt.Sprintf("%s/test.yaml", d)))
	})

	It("renders a templated kairos.config_url before handing it to CloudInitRunner", func() {
		// The kernel-cmdline path in yip's FromUrl fetches the URI verbatim,
		// so any {{ ... }} markers must be resolved BEFORE handoff. We use
		// a bare string literal instead of a sysinfo field here so the test
		// is fully deterministic on any host: the sysinfo-context path is
		// exercised at the SDK level with a mocked context. This spec's
		// job is only to prove RunStage invokes RenderConfigURL.
		// A pipe through Sprig's upper makes it obvious the template ran:
		// if RenderConfigURL was bypassed, the runner would see the raw
		// "{{ "hello" | upper }}" markers and this assertion would fail
		// with an unmistakable diff.
		templated := `http://d/?h={{ "hello" | upper }}`
		rendered := `http://d/?h=HELLO`

		mock := &argsRecordingCIRunner{}
		config.CloudInitRunner = mock

		Expect(writeCmdline(fmt.Sprintf(`kairos.config_url=%q`, templated), fs)).To(Succeed())

		Expect(utils.RunStage(config, "padme")).To(BeNil())

		// Collect every distinct arg the runner received. RunStage also
		// forwards the raw /proc/cmdline as a source to the dot-notation
		// modifier pass, so a substring check would be too strict; we
		// assert on the standalone config_url arg.
		flat := []string{}
		for _, tuple := range mock.Args {
			flat = append(flat, tuple...)
		}
		// The rendered URL must reach the runner as its own arg.
		Expect(flat).To(ContainElement(rendered))
		// The templated URL must never reach the runner as its own arg.
		Expect(flat).ToNot(ContainElement(templated))
	})

	It("drops the cmdline URI but still runs the cloud-init paths when the template references an undefined field", func() {
		// Regression: a bad template on the cmdline used to return from
		// runstage before the cloud-init path loop. Hook() then discarded
		// the error on a non-strict run, so every stage from every file
		// under /oem, /etc/kairos and /system/oem was skipped in silence.
		// One unrenderable cmdline token must never disable the file-based
		// configs, which are the real config channel.
		mock := &argsRecordingCIRunner{}
		config.CloudInitRunner = mock
		config.Strict = true

		templated := `http://d/?u={{.Values.definitely_not_a_key}}`
		Expect(writeCmdline(fmt.Sprintf(`kairos.config_url=%q`, templated), fs)).To(Succeed())

		err := utils.RunStage(config, "padme")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rendering config_url"))
		Expect(err.Error()).To(ContainSubstring("from cmdline"))
		Expect(err.Error()).To(ContainSubstring("definitely_not_a_key"))

		Expect(mock.ExecStages).To(ContainElements("padme.before", "padme", "padme.after"))

		flat := []string{}
		for _, tuple := range mock.Args {
			flat = append(flat, tuple...)
		}
		// A default cloud-init path only reaches the runner from the file
		// loop, so this is what proves the loop was not skipped.
		Expect(flat).To(ContainElement("/system/oem"))

		// The templated URI must never have reached the runner as a
		// standalone source arg. The raw /proc/cmdline arg forwarded to
		// the dot-notation pass legitimately contains the template as a
		// substring, so we only assert on element equality.
		Expect(flat).ToNot(ContainElement(templated))
	})

	It("leaves a non-templated kairos.config_url unchanged when handing it to CloudInitRunner", func() {
		// Regression: URLs without template markers hit the fast path in
		// RenderConfigURL and must reach the runner byte-for-byte identical.
		mock := &argsRecordingCIRunner{}
		config.CloudInitRunner = mock

		plain := "http://d/plain.yaml"
		Expect(writeCmdline(fmt.Sprintf("root=LABEL=X kairos.config_url=%s quiet", plain), fs)).To(Succeed())

		Expect(utils.RunStage(config, "padme")).To(BeNil())

		flat := []string{}
		for _, tuple := range mock.Args {
			flat = append(flat, tuple...)
		}
		Expect(flat).To(ContainElement(plain))
	})

	It("ignores kairos.config=key=value when resolving the URI", func() {
		// kairos.config=KEY=VALUE stanzas are consumed by the config
		// collector, not by RunStage — no yip source should be added.
		writeCmdline("kairos.config=install.auto=true kairos.config=users.0.name=kairos", fs)
		Expect(utils.RunStage(config, "padme")).To(BeNil())
		Expect(memLog.String()).ToNot(ContainSubstring("Found Kairos config URI on cmdline"))
	})

	It("parses cmdline uri with dotnotation", func() {
		writeCmdline("stages.leia[0].commands[0]='echo beepboop'", fs)
		config.Logger.SetLevel("debug")
		Expect(utils.RunStage(config, "leia")).To(BeNil())
		Expect(memLog).To(ContainSubstring("leia"))
		Expect(memLog).To(ContainSubstring("running command `echo beepboop`"))

		// try with a non-clean cmdline
		writeCmdline("BOOT=death-star single stages.leia[0].commands[0]='echo beepboop'", fs)
		Expect(utils.RunStage(config, "leia")).To(BeNil())
		Expect(memLog).To(ContainSubstring("leia"))
		Expect(memLog).To(ContainSubstring("running command `echo beepboop`"))
		Expect(memLog.String()).ToNot(ContainSubstring("/proc/cmdline parsing returned errors while unmarshalling"))
		Expect(memLog.String()).ToNot(ContainSubstring("Some errors found but were ignored. Enable --strict mode to fail on those or --debug to see them in the log"))
	})

	It("tolerates malformed cmdline tokens without user-visible errors", func() {
		// Old yip schema.DotNotationModifier turned garbage tokens into broken
		// YAML which yip then surfaced as yaml.TypeError. The SDK-backed
		// modifier (kairos-sdk PR #812) filters and skips those tokens
		// upstream, so RunStage completes cleanly. The user-visible contract
		// is unchanged: no strict-mode surfacing on non-strict runs.
		config.Logger.SetLevel("debug")
		writeCmdline("BOOT=death-star sing1!~@$%6^&**le /varlib stag_#var<Lib stages[0]='utterly broken by breaking schema'", fs)
		Expect(utils.RunStage(config, "leia")).To(BeNil())
		Expect(memLog.String()).ToNot(ContainSubstring("Some errors found but were ignored. Enable --strict mode to fail on those or --debug to see them in the log"))
	})

	It("fails in strict mode with non-yaml cloud init errors", func() {
		config.Logger.SetLevel("debug")
		config.Strict = true
		config.CloudInitRunner = &multiErrorCIRunner{}
		// A cos.setup stanza triggers the extra cmdline stage runs too
		writeCmdline("cos.setup=/some/file.yaml", fs)
		// An uncreatable cloud-init path exercises the debug log branch
		Expect(fs.WriteFile("/blocked-ci", []byte(""), os.ModePerm)).To(Succeed())
		config.CloudInitPaths = []string{"/blocked-ci/path"}

		err := utils.RunStage(config, "jarjar")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("generic cloud init failure"))
		Expect(memLog.String()).To(ContainSubstring("Failed creating cloud-init config path"))
	})

	It("analyzes the stages without running them", func() {
		config.Logger.SetLevel("debug")
		mock := &v1mock.FakeCloudInitRunner{}
		config.CloudInitRunner = mock

		Expect(utils.RunStageAnalyze(config, "obiwan")).To(BeNil())
		Expect(memLog.String()).To(ContainSubstring("Analyze mode, showing DAG"))
		// Analyze must not run any stage
		Expect(mock.ExecStages).To(BeEmpty())
	})

	It("analyzes the stages when a cos.setup stanza is present", func() {
		config.Logger.SetLevel("debug")
		mock := &v1mock.FakeCloudInitRunner{}
		config.CloudInitRunner = mock
		writeCmdline("cos.setup=/some/file.yaml", fs)

		Expect(utils.RunStageAnalyze(config, "anakin")).To(BeNil())
		Expect(memLog.String()).To(ContainSubstring("Analyze mode, showing DAG"))
		Expect(mock.ExecStages).To(BeEmpty())
	})
})
