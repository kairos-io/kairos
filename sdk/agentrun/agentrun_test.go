package agentrun_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos-sdk/agentrun"
	"github.com/kairos-io/kairos-sdk/constants"
)

var _ = Describe("agentrun", func() {
	Describe("ResolveAgentBin", func() {
		It("prefers KAIROS_AGENT_BIN when the file exists", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "fake-agent")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
			GinkgoT().Setenv(agentrun.EnvAgentBin, bin)
			Expect(agentrun.ResolveAgentBin()).To(Equal(bin))
		})

		It("falls back to kairos-agent on PATH when AgentDefaultPath is absent", func() {
			if _, err := os.Stat(constants.AgentDefaultPath); err == nil {
				Skip(constants.AgentDefaultPath + " is present on this system")
			}
			GinkgoT().Setenv(agentrun.EnvAgentBin, "")
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "kairos-agent")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
			GinkgoT().Setenv("PATH", dir)
			Expect(agentrun.ResolveAgentBin()).To(Equal(bin))
		})

		It("returns empty when nothing is found", func() {
			if _, err := os.Stat(constants.AgentDefaultPath); err == nil {
				Skip(constants.AgentDefaultPath + " is present on this system")
			}
			GinkgoT().Setenv(agentrun.EnvAgentBin, "")
			GinkgoT().Setenv("PATH", GinkgoT().TempDir())
			Expect(agentrun.ResolveAgentBin()).To(BeEmpty())
		})
	})

	Describe("Command", func() {
		It("builds manual-install with source, finish flag, default dirs and progress env", func() {
			cmd := agentrun.Command("/usr/bin/kairos-agent", "/tmp/cc.yaml", "oci://x:y", "reboot")
			Expect(cmd.Args).To(Equal([]string{
				"/usr/bin/kairos-agent", "manual-install",
				"--source", "oci://x:y",
				"--use-default-dirs",
				"--reboot",
				"/tmp/cc.yaml",
			}))
			Expect(cmd.Env).To(ContainElement("KAIROS_AGENT_PROGRESS=1"))
		})

		It("omits the finish flag when action is nothing", func() {
			cmd := agentrun.Command("/usr/bin/kairos-agent", "/tmp/cc.yaml", "", "nothing")
			Expect(cmd.Args).To(Equal([]string{
				"/usr/bin/kairos-agent", "manual-install",
				"--use-default-dirs",
				"/tmp/cc.yaml",
			}))
		})

		It("uses --poweroff for the poweroff action", func() {
			cmd := agentrun.Command("/usr/bin/kairos-agent", "/tmp/cc.yaml", "", "poweroff")
			Expect(cmd.Args).To(ContainElement("--poweroff"))
		})
	})

	Describe("ParseLine", func() {
		It("parses a step event", func() {
			ev, ok := agentrun.ParseLine([]byte(`{"event":"step","step":"partition"}`))
			Expect(ok).To(BeTrue())
			Expect(ev.Event).To(Equal("step"))
			Expect(ev.Step).To(Equal("partition"))
		})

		It("parses an error event", func() {
			ev, ok := agentrun.ParseLine([]byte(`{"event":"error","message":"boom"}`))
			Expect(ok).To(BeTrue())
			Expect(ev.Event).To(Equal("error"))
			Expect(ev.Message).To(Equal("boom"))
		})

		It("rejects a plain log line", func() {
			_, ok := agentrun.ParseLine([]byte(`time=... level=info msg=hello`))
			Expect(ok).To(BeFalse())
		})

		It("rejects JSON without an event field", func() {
			_, ok := agentrun.ParseLine([]byte(`{"level":"info","message":"hi"}`))
			Expect(ok).To(BeFalse())
		})
	})

	Describe("Run", func() {
		It("streams events and reports a clean exit", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "kairos-agent")
			script := "#!/bin/sh\n" +
				`echo '{"event":"step","step":"partition"}'` + "\n" +
				`echo 'some plain log line'` + "\n" +
				`echo '{"event":"step","step":"done"}'` + "\n" +
				"exit 0\n"
			Expect(os.WriteFile(bin, []byte(script), 0o755)).To(Succeed())

			var steps []string
			err := agentrun.Run(bin, "/tmp/cc.yaml", "", "nothing",
				func(ev agentrun.ProgressEvent) {
					if ev.Event == "step" {
						steps = append(steps, ev.Step)
					}
				},
				func(string) {},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(steps).To(Equal([]string{"partition", "done"}))
		})

		It("returns the exit error when the agent fails", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "kairos-agent")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\nexit 5\n"), 0o755)).To(Succeed())
			err := agentrun.Run(bin, "/tmp/cc.yaml", "", "nothing",
				func(agentrun.ProgressEvent) {}, func(string) {})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("RunWithOutput", func() {
		It("tees the full agent transcript (raw stdout + stderr) into out", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "kairos-agent")
			script := "#!/bin/sh\n" +
				`echo '{"event":"step","step":"partition"}'` + "\n" +
				"echo 'plain log line'\n" +
				"echo 'a stderr warning' 1>&2\n" +
				`echo '{"event":"step","step":"done"}'` + "\n" +
				"exit 0\n"
			Expect(os.WriteFile(bin, []byte(script), 0o755)).To(Succeed())

			var out strings.Builder
			var steps, logs []string
			err := agentrun.RunWithOutput(bin, "/tmp/cc.yaml", "", "nothing",
				func(ev agentrun.ProgressEvent) {
					if ev.Event == agentrun.EventStep {
						steps = append(steps, ev.Step)
					}
				},
				func(line string) { logs = append(logs, line) },
				&out,
			)
			Expect(err).ToNot(HaveOccurred())

			// Callbacks still fire exactly as with Run.
			Expect(steps).To(Equal([]string{"partition", "done"}))
			Expect(logs).To(Equal([]string{"plain log line"}))

			// out captures the complete transcript: raw stdout (progress JSON
			// included) AND stderr.
			transcript := out.String()
			Expect(transcript).To(ContainSubstring(`{"event":"step","step":"partition"}`))
			Expect(transcript).To(ContainSubstring("plain log line"))
			Expect(transcript).To(ContainSubstring(`{"event":"step","step":"done"}`))
			Expect(transcript).To(ContainSubstring("a stderr warning"))
		})

		It("still parses events when out is nil", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "kairos-agent")
			script := "#!/bin/sh\n" +
				`echo '{"event":"step","step":"done"}'` + "\n" +
				"exit 0\n"
			Expect(os.WriteFile(bin, []byte(script), 0o755)).To(Succeed())

			var steps []string
			err := agentrun.RunWithOutput(bin, "/tmp/cc.yaml", "", "nothing",
				func(ev agentrun.ProgressEvent) { steps = append(steps, ev.Step) },
				func(string) {},
				nil,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(steps).To(Equal([]string{"done"}))
		})

		It("returns the exit error when the agent fails", func() {
			dir := GinkgoT().TempDir()
			bin := filepath.Join(dir, "kairos-agent")
			Expect(os.WriteFile(bin, []byte("#!/bin/sh\nexit 5\n"), 0o755)).To(Succeed())
			var out strings.Builder
			err := agentrun.RunWithOutput(bin, "/tmp/cc.yaml", "", "nothing",
				func(agentrun.ProgressEvent) {}, func(string) {}, &out)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("contract constants", func() {
	It("exposes the step vocabulary in emission order", func() {
		Expect(agentrun.Steps).To(Equal([]string{
			"partition", "before-install", "active", "bootloader",
			"recovery", "passive", "after-install", "done",
		}))
	})
	It("names the progress env var and event values", func() {
		Expect(agentrun.EnvProgress).To(Equal("KAIROS_AGENT_PROGRESS"))
		Expect(agentrun.EventStep).To(Equal("step"))
		Expect(agentrun.EventError).To(Equal("error"))
	})
	It("names the agent install contract", func() {
		Expect(constants.AgentBinName).To(Equal("kairos-agent"))
		Expect(constants.AgentDefaultPath).To(Equal("/usr/bin/kairos-agent"))
		Expect(agentrun.EnvAgentBin).To(Equal(constants.AgentEnvVar))
	})
})
