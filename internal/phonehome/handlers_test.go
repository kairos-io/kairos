package phonehome

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/mount-utils"
)

var _ = Describe("artifact upgrade download", func() {
	originalPersistentDir := persistentDir

	AfterEach(func() {
		persistentDir = originalPersistentDir
	})

	It("creates the temporary image on the mounted persistent partition", func() {
		persistentDir = GinkgoT().TempDir()
		config := &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
			{Device: "/dev/persistent", Path: persistentDir},
		})}

		file, err := createArtifactTempFile(config, "artifact-123")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(file.Name()) }()
		defer func() { _ = file.Close() }()

		Expect(filepath.Dir(file.Name())).To(Equal(filepath.Join(persistentDir, "tmp")))
		Expect(filepath.Base(file.Name())).To(HavePrefix("phonehome-upgrade-artifact-123-"))
		Expect(filepath.Ext(file.Name())).To(Equal(".tar"))
	})

	It("refuses to download when the persistent partition is not mounted", func() {
		persistentDir = GinkgoT().TempDir()
		config := &sdkConfig.Config{Mounter: mount.NewFakeMounter(nil)}

		_, err := createArtifactTempFile(config, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("is not mounted")))
	})

	It("refuses to download when the persistent partition mount cannot be verified", func() {
		_, err := createArtifactTempFile(nil, "artifact-123")

		Expect(err).To(MatchError("cannot verify the persistent partition mount"))
	})

	It("reports an error when the persistent temporary directory cannot be created", func() {
		tempDir := GinkgoT().TempDir()
		persistentDir = filepath.Join(tempDir, "persistent")
		Expect(os.WriteFile(persistentDir, []byte("not a directory"), 0600)).To(Succeed())
		config := &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
			{Device: "/dev/persistent", Path: persistentDir},
		})}

		_, err := createArtifactTempFile(config, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("creating persistent temporary directory")))
	})

	newMountedConfig := func() *sdkConfig.Config {
		persistentDir = GinkgoT().TempDir()
		return &sdkConfig.Config{Mounter: mount.NewFakeMounter([]mount.MountPoint{
			{Device: "/dev/persistent", Path: persistentDir},
		})}
	}

	It("retries transient HTTP failures and succeeds", func() {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) < 3 {
				http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("artifact image"))
		}))
		defer server.Close()

		tarPath, err := downloadArtifact(
			context.Background(), server.URL, "api-key", "artifact-123",
			newMountedConfig(), 2, time.Millisecond,
		)

		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(tarPath) }()
		Expect(attempts.Load()).To(Equal(int32(3)))
		Expect(os.ReadFile(tarPath)).To(Equal([]byte("artifact image")))
	})

	It("retries transient network errors and succeeds", func() {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) == 1 {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			_, _ = w.Write([]byte("artifact image"))
		}))
		defer server.Close()

		tarPath, err := downloadArtifact(
			context.Background(), server.URL, "api-key", "artifact-123",
			newMountedConfig(), 1, time.Millisecond,
		)

		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(tarPath) }()
		Expect(attempts.Load()).To(Equal(int32(2)))
		Expect(os.ReadFile(tarPath)).To(Equal([]byte("artifact image")))
	})

	It("returns the final transient error after retries are exhausted", func() {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempt := attempts.Add(1)
			http.Error(w, fmt.Sprintf("failure %d", attempt), http.StatusServiceUnavailable)
		}))
		defer server.Close()

		_, err := downloadArtifact(
			context.Background(), server.URL, "api-key", "artifact-123",
			newMountedConfig(), 2, time.Millisecond,
		)

		Expect(attempts.Load()).To(Equal(int32(3)))
		Expect(err).To(MatchError(And(
			ContainSubstring("after 3 attempts"),
			ContainSubstring("HTTP 503"),
		)))
	})

	It("does not retry non-transient HTTP 4xx responses", func() {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			attempts.Add(1)
			http.NotFound(w, nil)
		}))
		defer server.Close()

		_, err := downloadArtifact(
			context.Background(), server.URL, "api-key", "artifact-123",
			newMountedConfig(), 5, time.Millisecond,
		)

		Expect(attempts.Load()).To(Equal(int32(1)))
		Expect(err).To(MatchError(ContainSubstring("HTTP 404")))
	})
})

var _ = Describe("handleReset", func() {
	var (
		originalSelectBootEntry = selectBootEntry
		originalRebootScheduler = rebootScheduler
	)

	AfterEach(func() {
		selectBootEntry = originalSelectBootEntry
		rebootScheduler = originalRebootScheduler
	})

	It("fails without the scanned system configuration", func() {
		_, err := handleReset(CommandData{}, nil)

		Expect(err).To(MatchError(ContainSubstring("scanned system configuration")))
	})

	It("selects the automatic state-reset entry before scheduling a reboot", func() {
		cfg := &sdkConfig.Config{}
		selected := ""
		rebootScheduled := false
		selectBootEntry = func(actualConfig *sdkConfig.Config, entry string) error {
			Expect(actualConfig).To(BeIdenticalTo(cfg))
			selected = entry
			return nil
		}
		rebootScheduler = func() { rebootScheduled = true }

		message, err := handleReset(CommandData{}, cfg)

		Expect(err).ToNot(HaveOccurred())
		Expect(selected).To(Equal("statereset"))
		Expect(rebootScheduled).To(BeTrue())
		Expect(message).To(ContainSubstring("Rebooting"))
	})

	It("is dispatched by the default command handler", func() {
		cfg := &sdkConfig.Config{}
		selectBootEntry = func(*sdkConfig.Config, string) error { return nil }
		rebootScheduler = func() {}
		handler := DefaultCommandHandler("http://example", func() string { return "" }, func(command string) bool {
			return command == "reset"
		}, nil, cfg)

		message, err := handler(CommandData{Command: "reset"})

		Expect(err).ToNot(HaveOccurred())
		Expect(message).To(ContainSubstring("Automatic state reset selected"))
	})

	DescribeTable("rejects unsupported legacy arguments without scheduling a reset",
		func(argument string) {
			selectBootEntry = func(*sdkConfig.Config, string) error {
				Fail("boot entry must not be selected")
				return nil
			}
			rebootScheduler = func() { Fail("reboot must not be scheduled") }
			handler := DefaultCommandHandler("http://example", func() string { return "" }, func(string) bool {
				return true
			}, nil, &sdkConfig.Config{})

			_, err := handler(CommandData{Command: "reset", Args: map[string]string{argument: "value"}})

			Expect(err).To(MatchError(ContainSubstring("is not supported")))
		},
		Entry("reset-oem", "reset-oem"),
		Entry("config", "config"),
	)

	It("does not reboot when selecting the state-reset entry fails", func() {
		selectBootEntry = func(*sdkConfig.Config, string) error { return errors.New("selection failed") }
		rebootScheduler = func() { Fail("reboot must not be scheduled") }

		_, err := handleReset(CommandData{}, &sdkConfig.Config{})

		Expect(err).To(MatchError(ContainSubstring("selection failed")))
	})
})
