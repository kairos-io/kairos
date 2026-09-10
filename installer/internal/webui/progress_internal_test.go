package webui

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("progressLog", func() {
	It("replays everything published before a reader arrived", func() {
		log := newProgressLog()
		log.publish(Message{Type: MessageStep, Step: "partition"})
		log.publish(Message{Type: MessageLog, Message: "hello"})

		msgs, next, done, _ := log.since(0)
		Expect(msgs).To(HaveLen(2))
		Expect(next).To(Equal(2))
		Expect(done).To(BeFalse())
	})

	It("hands a reader only what it has not seen", func() {
		log := newProgressLog()
		log.publish(Message{Type: MessageLog, Message: "one"})
		_, next, _, _ := log.since(0)

		log.publish(Message{Type: MessageLog, Message: "two"})
		msgs, _, _, _ := log.since(next)
		Expect(msgs).To(HaveLen(1))
		Expect(msgs[0].Message).To(Equal("two"))
	})

	It("tells a reader it is done only when it has been handed the last message", func() {
		log := newProgressLog()
		log.publish(Message{Type: MessageLog, Message: "one"})

		// Still going: nothing has ended the run.
		msgs, next, done, _ := log.since(0)
		Expect(msgs).To(HaveLen(1))
		Expect(done).To(BeFalse())

		log.publish(Message{Type: MessageDone, OK: true})

		// done comes back with the messages it applies to, not after them,
		// so the socket closes having sent the whole run.
		msgs, next, done, _ = log.since(next)
		Expect(msgs).To(HaveLen(1))
		Expect(done).To(BeTrue())

		// And a reader that arrives after everything gets the lot in one go.
		msgs, _, done, _ = log.since(0)
		Expect(msgs).To(HaveLen(2))
		Expect(done).To(BeTrue())
	})

	It("wakes a waiting reader on the next publish", func() {
		log := newProgressLog()
		_, _, _, changed := log.since(0)
		Expect(changed).ToNot(BeClosed())

		log.publish(Message{Type: MessageLog, Message: "one"})
		Eventually(changed).Should(BeClosed())
	})

	It("resumes a reader whose position fell out of the replay window", func() {
		log := newProgressLog()
		for i := 0; i < maxHistory+10; i++ {
			log.publish(Message{Type: MessageLog, Message: "line"})
		}

		// Index 0 is long gone. The reader must be moved up to the oldest
		// message still held rather than reading off the front of the slice.
		msgs, next, _, _ := log.since(0)
		Expect(msgs).To(HaveLen(maxHistory))
		Expect(next).To(Equal(maxHistory + 10))
	})

	It("reports running and succeeded from the done message", func() {
		log := newProgressLog()
		Expect(log.running()).To(BeTrue())
		Expect(log.succeeded()).To(BeFalse())

		log.publish(Message{Type: MessageDone, OK: true})
		Expect(log.running()).To(BeFalse())
		Expect(log.succeeded()).To(BeTrue())
	})

	It("does not report a failed run as succeeded", func() {
		log := newProgressLog()
		log.publish(Message{Type: MessageError, Message: "boom"})
		log.publish(Message{Type: MessageDone, OK: false})
		Expect(log.running()).To(BeFalse())
		Expect(log.succeeded()).To(BeFalse())
	})
})

var _ = Describe("renderCloudConfig", func() {
	It("writes the device the form named", func() {
		out, err := renderCloudConfig("#cloud-config\nusers:\n  - name: kairos\n", "/dev/sda")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(HavePrefix("#cloud-config\n"))
		Expect(out).To(ContainSubstring("device: /dev/sda"))
		Expect(out).To(ContainSubstring("name: kairos"))
	})

	It("overrides a device left in the pasted config", func() {
		// The dropdown is what the operator confirmed. A stale device in
		// the YAML must not send the install to a different disk.
		out, err := renderCloudConfig("#cloud-config\ninstall:\n  device: /dev/vdb\n  auto: true\n", "/dev/sda")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("device: /dev/sda"))
		Expect(out).ToNot(ContainSubstring("/dev/vdb"))
		Expect(out).To(ContainSubstring("auto: true"))
	})

	It("leaves the config alone when no device was given", func() {
		out, err := renderCloudConfig("#cloud-config\ninstall:\n  device: /dev/vdb\n", "")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("device: /dev/vdb"))
	})

	It("still produces a config when the form was submitted empty", func() {
		out, err := renderCloudConfig("", "auto")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("device: auto"))
	})

	It("reports YAML that does not parse", func() {
		_, err := renderCloudConfig("#cloud-config\nusers: [unterminated\n", "/dev/sda")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not valid YAML"))
	})
})

var _ = Describe("finishAction", func() {
	It("is reboot when both boxes are ticked", func() {
		// The agent's Lifecycle hook reboots before it looks at poweroff,
		// so this is what the machine actually does.
		Expect(finishAction("on", "on")).To(Equal("reboot"))
	})

	It("is poweroff when only power off is ticked", func() {
		Expect(finishAction("", "on")).To(Equal("poweroff"))
	})

	It("is nothing when neither is ticked", func() {
		Expect(finishAction("", "")).To(BeEmpty())
	})
})

var _ = Describe("startInstall", func() {
	It("refuses to write the cloud-config when there is no agent to read it", func() {
		// The config carries the operator's password hash, so a missing
		// agent must not leave one behind in the temp dir.
		tmp := GinkgoT().TempDir()
		GinkgoT().Setenv("TMPDIR", tmp)
		GinkgoT().Setenv("KAIROS_AGENT_BIN", filepath.Join(tmp, "does-not-exist"))
		GinkgoT().Setenv("PATH", tmp)

		err := startInstall(newProgressLog(), "", "#cloud-config\n", "/dev/sda", "")
		Expect(err).To(MatchError(errNoAgent))

		left, gerr := filepath.Glob(filepath.Join(tmp, "install-webui-*.yaml"))
		Expect(gerr).ToNot(HaveOccurred())
		Expect(left).To(BeEmpty())
	})

	It("streams the agent's steps, its plain output and a done message", func() {
		log := newProgressLog()
		bin := stubAgent(`
echo '{"event":"step","step":"partition"}'
echo 'a plain log line'
echo '{"event":"step","step":"done"}'
exit 0
`)
		GinkgoT().Setenv("KAIROS_AGENT_BIN", bin)

		Expect(startInstall(log, "", "#cloud-config\n", "/dev/sda", "")).To(Succeed())
		msgs := drain(log)

		Expect(msgs).To(ContainElement(Message{Type: MessageStep, Step: "partition"}))
		Expect(msgs).To(ContainElement(Message{Type: MessageStep, Step: "done"}))
		Expect(msgs).To(ContainElement(Message{Type: MessageLog, Message: "a plain log line"}))
		Expect(msgs[len(msgs)-1]).To(Equal(Message{Type: MessageDone, OK: true}))
	})

	It("reports an agent error and ends the run as failed", func() {
		log := newProgressLog()
		bin := stubAgent(`
echo '{"event":"error","message":"no such device"}'
exit 1
`)
		GinkgoT().Setenv("KAIROS_AGENT_BIN", bin)

		Expect(startInstall(log, "", "#cloud-config\n", "/dev/sda", "")).To(Succeed())
		msgs := drain(log)

		Expect(msgs).To(ContainElement(Message{Type: MessageError, Message: "no such device"}))
		Expect(msgs[len(msgs)-1]).To(Equal(Message{Type: MessageDone, OK: false}))
	})

	It("reports a non-zero exit the agent said nothing about", func() {
		// Without this the browser would see a run that just stopped.
		log := newProgressLog()
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent("exit 7\n"))

		Expect(startInstall(log, "", "#cloud-config\n", "/dev/sda", "")).To(Succeed())
		msgs := drain(log)

		Expect(msgs).To(HaveLen(2))
		Expect(msgs[0].Type).To(Equal(MessageError))
		Expect(msgs[0].Message).To(ContainSubstring("exit status 7"))
		Expect(msgs[1]).To(Equal(Message{Type: MessageDone, OK: false}))
	})

	It("hands the agent the device the form named", func() {
		log := newProgressLog()
		out := filepath.Join(GinkgoT().TempDir(), "seen.yaml")
		// The config file is the last argument; copy it out before the run
		// deletes it.
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
for a in "$@"; do cfg="$a"; done
cat "$cfg" > `+out+`
exit 0
`))

		Expect(startInstall(log, "", "#cloud-config\nusers:\n  - name: kairos\n", "/dev/sdb", "")).To(Succeed())
		drain(log)

		seen, err := os.ReadFile(out)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(seen)).To(ContainSubstring("device: /dev/sdb"))
		Expect(string(seen)).To(ContainSubstring("name: kairos"))
	})

	It("removes the cloud-config once the run is over", func() {
		tmp := GinkgoT().TempDir()
		GinkgoT().Setenv("TMPDIR", tmp)
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent("exit 0\n"))

		log := newProgressLog()
		Expect(startInstall(log, "", "#cloud-config\n", "/dev/sda", "")).To(Succeed())
		drain(log)

		left, err := filepath.Glob(filepath.Join(tmp, "install-webui-*.yaml"))
		Expect(err).ToNot(HaveOccurred())
		Expect(left).To(BeEmpty())
	})
})

// stubAgent writes a shell script standing in for kairos-agent and returns its
// path.
func stubAgent(body string) string {
	bin := filepath.Join(GinkgoT().TempDir(), "kairos-agent")
	Expect(os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755)).To(Succeed())
	return bin
}

// drain reads the log until the run publishes its done message and returns
// every message in order.
func drain(log *progressLog) []Message {
	var all []Message
	idx := 0
	for {
		msgs, next, done, changed := log.since(idx)
		all = append(all, msgs...)
		idx = next
		if done {
			return all
		}
		Eventually(changed, "30s").Should(BeClosed())
	}
}

var _ = Describe("the install source", func() {
	// The web UI is a frontend of the installer, so an install driven from
	// the browser has to honour the same --source the installer was started
	// with. Without it a `--no-tui` boot pointed at a private registry
	// silently installs whatever the submitted cloud-config names instead.
	recordArgs := func() string {
		out := filepath.Join(GinkgoT().TempDir(), "argv")
		GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`
printf '%s\n' "$@" > `+out+`
exit 0
`))
		return out
	}

	It("reaches the agent as --source", func() {
		log := newProgressLog()
		argv := recordArgs()

		Expect(startInstall(log, "oci://foo:bar", "#cloud-config\n", "/dev/sda", "")).To(Succeed())
		drain(log)

		seen, err := os.ReadFile(argv)
		Expect(err).ToNot(HaveOccurred())
		args := strings.Split(strings.TrimRight(string(seen), "\n"), "\n")
		Expect(args).To(ContainElement("--source"))
		Expect(args[indexOf(args, "--source")+1]).To(Equal("oci://foo:bar"))
	})

	It("is left out when the installer has none", func() {
		log := newProgressLog()
		argv := recordArgs()

		Expect(startInstall(log, "", "#cloud-config\n", "/dev/sda", "")).To(Succeed())
		drain(log)

		seen, err := os.ReadFile(argv)
		Expect(err).ToNot(HaveOccurred())
		Expect(strings.Split(string(seen), "\n")).ToNot(ContainElement("--source"))
	})

	// urfave/cli stops parsing flags at the first positional, so the config
	// path has to stay last whatever else is on the line.
	It("keeps the config path last, behind every flag", func() {
		log := newProgressLog()
		argv := recordArgs()

		Expect(startInstall(log, "oci://foo:bar", "#cloud-config\n", "/dev/vda", "reboot")).To(Succeed())
		drain(log)

		seen, err := os.ReadFile(argv)
		Expect(err).ToNot(HaveOccurred())
		args := strings.Split(strings.TrimRight(string(seen), "\n"), "\n")
		Expect(args).To(ContainElement("--reboot"))
		Expect(args[len(args)-1]).To(HaveSuffix(".yaml"))
		Expect(indexOf(args, "--reboot")).To(BeNumerically("<", len(args)-1))
	})
})

// indexOf returns the position of s in args, or -1.
func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}
