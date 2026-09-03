package phonehome

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// commandRecorder captures shell-outs and returns a command that always exits
// successfully. Specs that need a failure use failingRecorder instead.
type commandRecorder struct {
	calls [][]string
}

func (r *commandRecorder) record(name string, args ...string) *exec.Cmd {
	r.calls = append(r.calls, append([]string{name}, args...))
	return exec.Command("/bin/true")
}

// failingRecorder is commandRecorder with one nominated call (0-based) exiting
// non-zero, so a spec can pin down which step of a multi-step action failed.
type failingRecorder struct {
	calls  [][]string
	failOn int
}

func (r *failingRecorder) record(name string, args ...string) *exec.Cmd {
	idx := len(r.calls)
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.failOn == idx {
		return exec.Command("/bin/false")
	}
	return exec.Command("/bin/true")
}

// withRecordedCommands points execCommand at rec for the duration of the spec.
func withRecordedCommands(rec func(string, ...string) *exec.Cmd) func() {
	prev := execCommand
	execCommand = rec
	return func() { execCommand = prev }
}

// withExtensionRoot points the scope-dir lookup at a temp tree laid out the
// same way as /var/lib/kairos: <root>/<type>s/<scope>/.
func withExtensionRoot(root string) func() {
	prev := extensionScopeDir
	extensionScopeDir = func(bootState, extType string) string {
		return filepath.Join(root, extType+"s", bootState)
	}
	return func() { extensionScopeDir = prev }
}

func withRebootScheduler(fn func()) func() {
	prev := rebootScheduler
	rebootScheduler = fn
	return func() { rebootScheduler = prev }
}

var _ = Describe("parseExtensionArgs", func() {
	It("parses a complete install request", func() {
		got, err := parseExtensionArgs(map[string]string{
			"type":      "sysext",
			"action":    "install",
			"name":      "tailscale-agent",
			"source":    "https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
			"bootState": "common",
			"now":       "true",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.Type).To(Equal("sysext"))
		Expect(got.Action).To(Equal("install"))
		Expect(got.Name).To(Equal("tailscale-agent"))
		Expect(got.BootState).To(Equal("common"))
		Expect(got.Now).To(BeTrue())
	})

	It("rejects a missing type", func() {
		_, err := parseExtensionArgs(map[string]string{"action": "install"})
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("rejects an unsupported type", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "blob", "action": "install", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("requires source for action=install", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "install", "name": "x", "bootState": "common",
		})
		Expect(err).To(MatchError(ContainSubstring("source")))
	})

	It("requires bootState for action=enable", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "enable", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("requires bootState for action=disable", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "disable", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("does not require bootState for action=remove", func() {
		got, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "remove", "name": "x",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(got.BootState).To(BeEmpty())
	})

	It("requires name for every action", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "remove",
		})
		Expect(err).To(MatchError(ContainSubstring("name")))
	})

	It("rejects an unsupported bootState", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "enable", "name": "x", "bootState": "wat",
		})
		Expect(err).To(MatchError(ContainSubstring("bootState")))
	})

	It("rejects an unsupported action", func() {
		_, err := parseExtensionArgs(map[string]string{
			"type": "sysext", "action": "frobnicate", "name": "x",
		})
		Expect(err).To(MatchError(ContainSubstring("action")))
	})
})

var _ = Describe("DefaultCommandHandler, extension command", func() {
	It("rejects the command when isAllowed returns false", func() {
		denyAll := func(string) bool { return false }
		handler := DefaultCommandHandler("http://example", func() string { return "" }, denyAll, nil, nil)
		_, err := handler(CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{"type": "sysext", "action": "remove", "name": "x"},
		})
		Expect(err).To(MatchError(ContainSubstring("not permitted")))
	})

	It("surfaces parse errors when the args are malformed", func() {
		allow := func(string) bool { return true }
		handler := DefaultCommandHandler("http://example", func() string { return "" }, allow, nil, nil)
		_, err := handler(CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{"type": "wat"},
		})
		Expect(err).To(MatchError(ContainSubstring("unsupported type")))
	})

	It("dispatches to handleExtension when the args validate", func() {
		rec := &commandRecorder{}
		defer withRecordedCommands(rec.record)()
		allow := func(string) bool { return true }
		handler := DefaultCommandHandler("http://example", func() string { return "" }, allow, nil, nil)
		_, err := handler(CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "remove", "name": "tailscale-agent",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "remove", "tailscale-agent"},
		}))
	})
})

var _ = Describe("handleExtension, install action", func() {
	var rec *commandRecorder
	var restore func()

	BeforeEach(func() {
		rec = &commandRecorder{}
		restore = withRecordedCommands(rec.record)
	})
	AfterEach(func() { restore() })

	It("issues install then enable with --now when now=true", func() {
		out, err := handleExtension(context.Background(), CommandData{
			ID: "c1", Command: "extension",
			Args: map[string]string{
				"type":      "sysext",
				"action":    "install",
				"name":      "tailscale-agent",
				"source":    "https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
				"bootState": "common",
				"now":       "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring("installed"))
		Expect(rec.calls).To(HaveLen(2))
		Expect(rec.calls[0]).To(Equal([]string{
			"kairos-agent", "sysext", "install",
			"https://aurora/api/v1/extensions/abc/download/tailscale-agent.sysext.raw?token=k",
		}))
		Expect(rec.calls[1]).To(Equal([]string{
			"kairos-agent", "sysext", "enable", "--common", "--now", "tailscale-agent",
		}))
	})

	It("omits --now when now is unset", func() {
		_, err := handleExtension(context.Background(), CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "confext", "action": "install",
				"name":      "fluent-bit-config",
				"source":    "https://x/file?token=k",
				"bootState": "active",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(2))
		Expect(rec.calls[1]).To(Equal([]string{
			"kairos-agent", "confext", "enable", "--active", "fluent-bit-config",
		}))
	})
})

var _ = Describe("handleExtension, enable disable and remove", func() {
	var rec *commandRecorder
	var restore func()

	BeforeEach(func() {
		rec = &commandRecorder{}
		restore = withRecordedCommands(rec.record)
	})
	AfterEach(func() { restore() })

	It("issues enable without --now when now=false", func() {
		_, err := handleExtension(context.Background(), CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "enable",
				"name": "tailscale-agent", "bootState": "passive", "now": "false",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "enable", "--passive", "tailscale-agent"},
		}))
	})

	It("issues disable with --now when now=true", func() {
		_, err := handleExtension(context.Background(), CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "confext", "action": "disable",
				"name": "fluent-bit-config", "bootState": "common", "now": "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "confext", "disable", "--common", "--now", "fluent-bit-config"},
		}))
	})

	It("issues remove with --now when now=true", func() {
		_, err := handleExtension(context.Background(), CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "remove",
				"name": "tailscale-agent", "now": "true",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "remove", "--now", "tailscale-agent"},
		}))
	})
})

var _ = Describe("handleExtension, install error paths", func() {
	It("returns the install error and does not call enable when the download fails", func() {
		rec := &failingRecorder{failOn: 0}
		defer withRecordedCommands(rec.record)()

		_, err := handleExtension(context.Background(), CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "install",
				"name": "x", "source": "https://x/y", "bootState": "common",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extension install")))
		Expect(rec.calls).To(HaveLen(1))
	})

	It("returns the enable error when the symlink fails after a good download", func() {
		rec := &failingRecorder{failOn: 1}
		defer withRecordedCommands(rec.record)()

		_, err := handleExtension(context.Background(), CommandData{
			Command: "extension",
			Args: map[string]string{
				"type": "sysext", "action": "install",
				"name": "x", "source": "https://x/y", "bootState": "common",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extension enable")))
		Expect(rec.calls).To(HaveLen(2))
	})
})

var _ = Describe("parseBundledExtensions", func() {
	It("returns nothing for an empty input", func() {
		got, err := parseBundledExtensions("")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(BeEmpty())
	})

	It("decodes a well-formed array", func() {
		raw, _ := json.Marshal([]BundledExtension{
			{Type: "sysext", Name: "tailscale-agent", Source: "https://x/a"},
			{Type: "confext", Name: "fluent-bit-config", Source: "https://x/b"},
		})
		got, err := parseBundledExtensions(string(raw))
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(HaveLen(2))
		Expect(got[0].Name).To(Equal("tailscale-agent"))
		Expect(got[1].Type).To(Equal("confext"))
	})

	It("rejects an unsupported type", func() {
		_, err := parseBundledExtensions(`[{"type":"blob","name":"x","source":"https://x"}]`)
		Expect(err).To(MatchError(ContainSubstring("type")))
	})

	It("rejects a missing name", func() {
		_, err := parseBundledExtensions(`[{"type":"sysext","source":"https://x"}]`)
		Expect(err).To(MatchError(ContainSubstring("name")))
	})

	It("rejects a missing source", func() {
		_, err := parseBundledExtensions(`[{"type":"sysext","name":"x"}]`)
		Expect(err).To(MatchError(ContainSubstring("source")))
	})

	It("rejects malformed JSON", func() {
		_, err := parseBundledExtensions(`not json`)
		Expect(err).To(MatchError(ContainSubstring("extensions arg")))
	})
})

var _ = Describe("extensionEnabledAnywhere", func() {
	var restore func()
	var tmpRoot string

	BeforeEach(func() {
		tmpRoot = GinkgoT().TempDir()
		restore = withExtensionRoot(tmpRoot)
	})
	AfterEach(func() { restore() })

	It("returns false when no scope dir holds the extension", func() {
		Expect(extensionEnabledAnywhere("sysext", "tailscale-agent")).To(BeFalse())
	})

	It("returns true when an entry exists under active", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "active")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(extensionEnabledAnywhere("sysext", "tailscale-agent")).To(BeTrue())
	})

	It("returns true when an entry exists under common", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(extensionEnabledAnywhere("sysext", "tailscale-agent")).To(BeTrue())
	})

	It("matches on prefix plus a dot so a longer neighbour is not a false positive", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent-helper.sysext.raw"), nil, 0o644)).To(Succeed())
		Expect(extensionEnabledAnywhere("sysext", "tailscale-agent")).To(BeFalse())
	})

	It("does not confuse a confext with a sysext of the same name", func() {
		scopeDir := filepath.Join(tmpRoot, "confexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "shared.confext.raw"), nil, 0o644)).To(Succeed())
		Expect(extensionEnabledAnywhere("confext", "shared")).To(BeTrue())
		Expect(extensionEnabledAnywhere("sysext", "shared")).To(BeFalse())
	})
})

var _ = Describe("installBundledExtension", func() {
	var rec *commandRecorder
	var restoreExec, restoreRoot func()
	var tmpRoot string

	BeforeEach(func() {
		tmpRoot = GinkgoT().TempDir()
		restoreRoot = withExtensionRoot(tmpRoot)
		rec = &commandRecorder{}
		restoreExec = withRecordedCommands(rec.record)
	})
	AfterEach(func() {
		restoreExec()
		restoreRoot()
	})

	It("installs and enables a brand new extension at --active", func() {
		Expect(installBundledExtension(context.Background(),
			BundledExtension{Type: "sysext", Name: "tailscale-agent", Source: "https://x/y"},
			"active",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/y"},
			{"kairos-agent", "sysext", "enable", "--active", "tailscale-agent"},
		}))
	})

	It("skips enable when the extension is already present at common scope", func() {
		scopeDir := filepath.Join(tmpRoot, "sysexts", "common")
		Expect(os.MkdirAll(scopeDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(scopeDir, "tailscale-agent.sysext.raw"), nil, 0o644)).To(Succeed())

		Expect(installBundledExtension(context.Background(),
			BundledExtension{Type: "sysext", Name: "tailscale-agent", Source: "https://x/y"},
			"active",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/y"},
		}))
	})

	It("enables at the recovery scope when called with scope=recovery", func() {
		Expect(installBundledExtension(context.Background(),
			BundledExtension{Type: "sysext", Name: "rescue-tools", Source: "https://x/r"},
			"recovery",
		)).To(Succeed())
		Expect(rec.calls).To(Equal([][]string{
			{"kairos-agent", "sysext", "install", "https://x/r"},
			{"kairos-agent", "sysext", "enable", "--recovery", "rescue-tools"},
		}))
	})
})

var _ = Describe("handleUpgrade, extensions bundle", func() {
	var rec *commandRecorder
	var restoreExec, restoreRoot, restoreReboot func()
	var rebootCalled bool

	upgrade := func(cmd CommandData) (string, error) {
		return handleUpgrade(context.Background(), cmd, "", "", nil, 0, 0)
	}

	BeforeEach(func() {
		restoreRoot = withExtensionRoot(GinkgoT().TempDir())
		rec = &commandRecorder{}
		restoreExec = withRecordedCommands(rec.record)
		rebootCalled = false
		restoreReboot = withRebootScheduler(func() { rebootCalled = true })
	})
	AfterEach(func() {
		restoreExec()
		restoreRoot()
		restoreReboot()
	})

	It("leaves the upgrade untouched when the extensions arg is absent", func() {
		_, err := upgrade(CommandData{
			ID: "u1", Command: "upgrade",
			Args: map[string]string{"source": "oci:quay.io/myorg/edge-os:v4.2.0"},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(1))
		Expect(rec.calls[0]).To(ContainElement("upgrade"))
		Expect(rebootCalled).To(BeTrue())
	})

	It("installs every bundled extension before running the upgrade", func() {
		raw, _ := json.Marshal([]BundledExtension{
			{Type: "sysext", Name: "tailscale-agent", Source: "https://x/a"},
			{Type: "confext", Name: "fluent-bit-config", Source: "https://x/b"},
		})
		_, err := upgrade(CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": string(raw),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(5))
		Expect(rec.calls[0][1:3]).To(Equal([]string{"sysext", "install"}))
		Expect(rec.calls[1][1:3]).To(Equal([]string{"sysext", "enable"}))
		Expect(rec.calls[1]).To(ContainElement("--active"))
		Expect(rec.calls[2][1:3]).To(Equal([]string{"confext", "install"}))
		Expect(rec.calls[3][1:3]).To(Equal([]string{"confext", "enable"}))
		Expect(rec.calls[4]).To(ContainElement("upgrade"))
		Expect(rebootCalled).To(BeTrue())
	})

	It("aborts before the OS upgrade when an extension install fails", func() {
		failRec := &failingRecorder{failOn: 0}
		restoreExec()
		restoreExec = withRecordedCommands(failRec.record)

		raw, _ := json.Marshal([]BundledExtension{
			{Type: "sysext", Name: "x", Source: "https://x/a"},
		})
		_, err := upgrade(CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": string(raw),
			},
		})
		Expect(err).To(MatchError(ContainSubstring("install sysext/x")))
		Expect(failRec.calls).To(HaveLen(1))
		Expect(rebootCalled).To(BeFalse())
	})

	It("rejects a malformed extensions arg without touching the system", func() {
		_, err := upgrade(CommandData{
			Command: "upgrade",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"extensions": "not json",
			},
		})
		Expect(err).To(MatchError(ContainSubstring("extensions arg")))
		Expect(rec.calls).To(BeEmpty())
		Expect(rebootCalled).To(BeFalse())
	})

	It("enables bundled extensions at --recovery scope for upgrade-recovery", func() {
		raw, _ := json.Marshal([]BundledExtension{
			{Type: "sysext", Name: "rescue-tools", Source: "https://x/r"},
		})
		_, err := upgrade(CommandData{
			Command: "upgrade-recovery",
			Args: map[string]string{
				"source":     "oci:quay.io/myorg/edge-os:v4.2.0",
				"recovery":   "true",
				"extensions": string(raw),
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(rec.calls).To(HaveLen(3))
		Expect(rec.calls[1]).To(Equal([]string{"kairos-agent", "sysext", "enable", "--recovery", "rescue-tools"}))
		Expect(rebootCalled).To(BeFalse())
	})
})
