package e2e_test

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// deadVMSink is a VM that answers nothing, which is what a spec that just
// failed usually has in front of it.
func deadVMSink(ranCmds *[]string, fetched *[]string) logSink {
	return logSink{
		run: func(cmd string) (string, error) {
			*ranCmds = append(*ranCmds, cmd)
			return "", errors.New("ssh: connection refused")
		},
		fetch: func(remotePath, _ string) error {
			*fetched = append(*fetched, remotePath)
			return errors.New("ssh: connection refused")
		},
	}
}

var _ = Describe("gatherAllLogs", Label("e2e"), func() {
	var outDir string

	BeforeEach(func() {
		outDir = filepath.Join(GinkgoT().TempDir(), "logs")
	})

	It("collects every requested service and file, plus the system ones", func() {
		var ranCmds, fetched []string
		sink := logSink{
			run: func(cmd string) (string, error) {
				ranCmds = append(ranCmds, cmd)
				return "", nil
			},
			fetch: func(remotePath, localPath string) error {
				fetched = append(fetched, remotePath)
				return os.WriteFile(localPath, []byte(remotePath), 0644)
			},
		}

		gatherAllLogs(sink, outDir, []string{"kairos-agent"}, []string{"/var/log/kairos/agent.log"})

		Expect(ranCmds).To(ContainElement("journalctl -u kairos-agent -o short-iso >> /run/kairos-agent.log"))
		Expect(fetched).To(ContainElements(
			"/run/kairos-agent.log",
			"/var/log/kairos/agent.log",
			// peg's GatherAllLogs adds these on top of what the caller asks
			// for, so dropping it must not drop them.
			"/run/dmesg",
			"/run/journal.log",
			"/run/uname.log",
			"/run/disks.log",
			"/etc/passwd",
			"/etc/os-release",
		))

		// Every fetched file lands in outDir under its base name.
		Expect(filepath.Join(outDir, "agent.log")).To(BeAnExistingFile())
		Expect(os.ReadFile(filepath.Join(outDir, "os-release"))).To(Equal([]byte("/etc/os-release")))
	})

	It("chmods a file before fetching it", func() {
		var ranCmds, fetched []string
		sink := logSink{
			run: func(cmd string) (string, error) {
				ranCmds = append(ranCmds, cmd)
				return "", nil
			},
			fetch: func(remotePath, localPath string) error {
				fetched = append(fetched, remotePath)
				return os.WriteFile(localPath, nil, 0644)
			},
		}

		gatherAllLogs(sink, outDir, nil, []string{"/run/pods.json"})

		Expect(ranCmds).To(ContainElement("chmod 777 /run/pods.json"))
		Expect(fetched).To(ContainElement("/run/pods.json"))
	})

	// This is the regression. peg's vm.GatherAllLogs panics from a goroutine of
	// its own when the SSH session has died, and a panic Ginkgo does not own
	// ends the whole test binary: no spec failure, no AfterEach, no artifacts.
	It("does not panic, and keeps going, when the VM answers nothing", func() {
		var ranCmds, fetched []string
		sink := deadVMSink(&ranCmds, &fetched)

		Expect(func() {
			gatherAllLogs(sink, outDir, []string{"kairos-agent", "k3s"}, []string{"/run/pods.json", "/run/disk"})
		}).ToNot(Panic())

		// A failing chmod means the file cannot be read, so nothing is fetched,
		// but every later service and file is still attempted.
		Expect(fetched).To(BeEmpty())
		Expect(ranCmds).To(ContainElements(
			"journalctl -u kairos-agent -o short-iso >> /run/kairos-agent.log",
			"journalctl -u k3s -o short-iso >> /run/k3s.log",
			"chmod 777 /run/pods.json",
			"chmod 777 /run/disk",
			"chmod 777 /etc/os-release",
		))
	})

	It("does not panic when the VM answers but the copy fails", func() {
		var fetched []string
		sink := logSink{
			run: func(string) (string, error) { return "", nil },
			fetch: func(remotePath, _ string) error {
				fetched = append(fetched, remotePath)
				return errors.New("scp: broken pipe")
			},
		}

		Expect(func() {
			gatherAllLogs(sink, outDir, nil, []string{"/run/pods.json", "/run/disk"})
		}).ToNot(Panic())

		Expect(fetched).To(ContainElements("/run/pods.json", "/run/disk", "/etc/os-release"))
	})
})
