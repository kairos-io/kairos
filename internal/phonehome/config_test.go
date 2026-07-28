package phonehome_test

import (
	"os"
	"time"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// newCollectorConfig builds an sdkConfig.Config whose Collector renders
// to the given phonehome-scoped values. This is exactly the shape produced
// by a real `config.Scan` on cloud-config files — the helper just skips
// the file I/O so LoadFromCollector can be tested in isolation.
func newCollectorConfig(phonehome map[string]interface{}) *sdkConfig.Config {
	values := collector.ConfigValues{}
	if phonehome != nil {
		values["phonehome"] = phonehome
	}
	return &sdkConfig.Config{
		Collector: collector.Config{Values: values},
	}
}

var _ = Describe("LoadFromCollector", func() {
	It("extracts a phonehome section from a merged cloud-config", func() {
		c := newCollectorConfig(map[string]interface{}{
			"url":                              "https://example.invalid",
			"registration_token":               "reg-123",
			"group":                            "edge",
			"labels":                           map[string]interface{}{"env": "prod"},
			"allowed_commands":                 []interface{}{"exec", "reboot"},
			"artifact_download_retries":        7,
			"artifact_download_retry_interval": "2s",
		})

		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(cfg).ToNot(BeNil())
		Expect(cfg.URL).To(Equal("https://example.invalid"))
		Expect(cfg.RegistrationToken).To(Equal("reg-123"))
		Expect(cfg.Group).To(Equal("edge"))
		Expect(cfg.Labels).To(HaveKeyWithValue("env", "prod"))
		Expect(cfg.AllowedCommands).To(ConsistOf("exec", "reboot"))
		Expect(cfg.ArtifactDownloadRetries).To(Equal(7))
		Expect(cfg.ArtifactDownloadRetryInterval).To(Equal(2 * time.Second))
	})

	It("returns ok=false when the phonehome section is absent", func() {
		c := newCollectorConfig(nil)
		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(cfg).To(BeNil())
	})

	It("returns ok=false when phonehome.url is empty", func() {
		c := newCollectorConfig(map[string]interface{}{
			"group": "edge",
		})
		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(cfg).To(BeNil())
	})

	It("returns ok=false on a nil sdkConfig.Config pointer", func() {
		cfg, ok, err := phonehome.LoadFromCollector(nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeFalse())
		Expect(cfg).To(BeNil())
	})

	It("round-trips the full Config back through IsAllowed", func() {
		c := newCollectorConfig(map[string]interface{}{
			"url":              "https://example.invalid",
			"allowed_commands": []interface{}{"exec"},
		})
		cfg, ok, err := phonehome.LoadFromCollector(c)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		// The policy parsed out of the Collector must drive IsAllowed correctly.
		Expect(cfg.IsAllowed("exec")).To(BeTrue())
		Expect(cfg.IsAllowed("reboot")).To(BeFalse())
	})
})

var _ = Describe("Config.IsAllowed", func() {
	It("allows the default safe set when AllowedCommands is nil", func() {
		cfg := &phonehome.Config{}
		Expect(cfg.IsAllowed("upgrade")).To(BeTrue())
		Expect(cfg.IsAllowed("upgrade-recovery")).To(BeTrue())
		Expect(cfg.IsAllowed("reboot")).To(BeTrue())
	})

	It("denies destructive commands by default", func() {
		cfg := &phonehome.Config{}
		Expect(cfg.IsAllowed("exec")).To(BeFalse())
		Expect(cfg.IsAllowed("reset")).To(BeFalse())
		Expect(cfg.IsAllowed("apply-cloud-config")).To(BeFalse())
	})

	// unregister is a deliberate default-allow so that newly-registered
	// nodes can be cleanly decommissioned without first having to push a
	// cloud-config update that opts in to the teardown command. The
	// behaviour here is load-bearing for the "delete node" UX.
	It("allows `unregister` by default so decommission works out of the box", func() {
		cfg := &phonehome.Config{}
		Expect(cfg.IsAllowed("unregister")).To(BeTrue())
	})

	It("respects an explicit allowlist that omits `unregister`", func() {
		cfg := &phonehome.Config{AllowedCommands: []string{"upgrade"}}
		Expect(cfg.IsAllowed("unregister")).To(BeFalse())
	})

	It("honors an explicit AllowedCommands allowlist", func() {
		cfg := &phonehome.Config{AllowedCommands: []string{"exec"}}
		Expect(cfg.IsAllowed("exec")).To(BeTrue())
		// Explicit list replaces defaults — reboot is no longer allowed.
		Expect(cfg.IsAllowed("reboot")).To(BeFalse())
	})

	It("treats an empty (non-nil) slice as deny-all", func() {
		cfg := &phonehome.Config{AllowedCommands: []string{}}
		Expect(cfg.IsAllowed("upgrade")).To(BeFalse())
		Expect(cfg.IsAllowed("reboot")).To(BeFalse())
	})
})

var _ = Describe("DefaultCommandHandler policy gating", func() {
	It("rejects commands that are not in the allowlist", func() {
		cfg := &phonehome.Config{} // defaults — exec not allowed
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

		out, err := handler(phonehome.CommandData{
			ID:      "c1",
			Command: "exec",
			Args:    map[string]string{"command": "echo hi"},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not permitted"))
		Expect(out).To(BeEmpty())
	})

	It("rejects unknown commands even when listed in defaults", func() {
		cfg := &phonehome.Config{}
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, nil, nil)

		_, err := handler(phonehome.CommandData{ID: "c2", Command: "totally-made-up"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not permitted"))
	})

	It("denies everything when isAllowed is nil (fail-closed)", func() {
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, nil, nil, nil)

		_, err := handler(phonehome.CommandData{ID: "c3", Command: "upgrade"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not permitted"))
	})

	// The unregister path runs the real on-host teardown via Uninstall,
	// which would try to stop a systemd unit and manipulate /oem files.
	// Swap out all five indirections for the duration of this spec so the
	// test stays hermetic (no real systemctl calls, no real I/O).
	It("invokes the stop callback after a successful `unregister`", func() {
		restore := phonehome.SetUninstallRunners(
			func(name string, args ...string) ([]byte, error) { return nil, nil },
			func(string) error { return os.ErrNotExist },
			func(string) ([]byte, error) { return nil, os.ErrNotExist },
			func(string, []byte, os.FileMode) error { return nil },
			func(string) ([]string, error) { return nil, nil },
		)
		defer restore()

		stopped := make(chan struct{}, 1)
		stop := func() { stopped <- struct{}{} }
		cfg := &phonehome.Config{} // defaults include unregister
		handler := phonehome.DefaultCommandHandler("http://example", func() string { return "" }, cfg.IsAllowed, stop, nil)

		out, err := handler(phonehome.CommandData{ID: "u1", Command: "unregister"})
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring(phonehome.ServiceName))

		// Stop runs via time.AfterFunc(500ms); wait a bit longer than that.
		Eventually(stopped, "2s", "50ms").Should(Receive())
	})
})
