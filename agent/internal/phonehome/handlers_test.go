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

	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	ghwMock "github.com/kairos-io/kairos/v4/sdk/ghw/mocks"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkPartitions "github.com/kairos-io/kairos/v4/sdk/types/partitions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

var _ = Describe("artifact upgrade download", func() {
	// withPersistent fakes the disk layout ghw reads: COS_PERSISTENT mounted
	// at mountPoint, or not mounted when mountPoint is empty. A running
	// system has it at /usr/local, where immucore mounts it.
	withPersistent := func(mountPoint string) *sdkConfig.Config {
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: []*sdkPartitions.Partition{{
			Name:            "vda5",
			Path:            "/dev/vda5",
			FilesystemLabel: sdkConstants.PersistentLabel,
			FS:              "ext4",
			MountPoint:      mountPoint,
		}}})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)
		return &sdkConfig.Config{Logger: sdkLogger.NewNullLogger()}
	}

	It("creates the temporary image where the persistent partition is mounted", func() {
		mountPoint := GinkgoT().TempDir()
		config := withPersistent(mountPoint)

		file, err := createArtifactTempFile(config, "artifact-123")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(file.Name()) }()
		defer func() { _ = file.Close() }()

		Expect(filepath.Dir(file.Name())).To(Equal(filepath.Join(mountPoint, "tmp")))
		Expect(filepath.Base(file.Name())).To(HavePrefix("phonehome-upgrade-artifact-123-"))
		Expect(filepath.Ext(file.Name())).To(Equal(".tar"))
	})

	It("refuses to download when the persistent partition is not mounted", func() {
		config := withPersistent("")

		_, err := createArtifactTempFile(config, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("is not mounted")))
	})

	It("refuses to download when there is no persistent partition", func() {
		ghwTest := ghwMock.GhwMock{}
		ghwTest.AddDisk(sdkPartitions.Disk{Name: "vda", Partitions: []*sdkPartitions.Partition{{
			Name: "vda2", Path: "/dev/vda2", FilesystemLabel: sdkConstants.OEMLabel, FS: "ext4", MountPoint: "/oem",
		}}})
		ghwTest.CreateDevices()
		DeferCleanup(ghwTest.Clean)

		_, err := createArtifactTempFile(&sdkConfig.Config{Logger: sdkLogger.NewNullLogger()}, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("no persistent partition")))
	})

	// kcrypt encrypts COS_PERSISTENT: the label lives on the filesystem
	// inside the LUKS container, so the partition is only found through the
	// unlocked device-mapper device.
	It("creates the temporary image on an encrypted persistent partition", func() {
		mountPoint := GinkgoT().TempDir()
		// The mount point is read only once the parent disk resolves, and
		// it resolves on the real filesystem under /dev/disk/by-path.
		// Climbing out of that directory lands on a temporary one that
		// exists on any host.
		parentDisk := GinkgoT().TempDir()
		Expect(os.Setenv("GHW_CHROOT", "")).To(Succeed())
		DeferCleanup(func() { Expect(os.Unsetenv("GHW_CHROOT")).To(Succeed()) })
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{
			"/sys/block/dm-0/dev":             "253:0\n",
			"/sys/block/dm-0/slaves/vda5/dev": "8:5\n",
			"/run/udev/data/b253:0": fmt.Sprintf("E:ID_FS_LABEL=%s\nE:ID_FS_TYPE=ext4\nE:DM_NAME=vda5\n",
				sdkConstants.PersistentLabel),
			"/run/udev/data/b8:0": "E:ID_PATH=../../.." + parentDisk + "\n",
			"/proc/mounts":        fmt.Sprintf("/dev/mapper/vda5 %s ext4 rw 0 0\n", mountPoint),
		})
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(cleanup)

		file, err := createArtifactTempFile(&sdkConfig.Config{Logger: sdkLogger.NewNullLogger(), Fs: fs}, "artifact-123")
		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(file.Name()) }()
		defer func() { _ = file.Close() }()

		Expect(filepath.Dir(file.Name())).To(Equal(filepath.Join(mountPoint, "tmp")))
	})

	It("refuses to download when the persistent partition cannot be looked up", func() {
		_, err := createArtifactTempFile(nil, "artifact-123")

		Expect(err).To(MatchError("cannot look up the persistent partition"))
	})

	It("reports an error when the persistent temporary directory cannot be created", func() {
		mountPoint := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(mountPoint, "tmp"), []byte("not a directory"), 0600)).To(Succeed())
		config := withPersistent(mountPoint)

		_, err := createArtifactTempFile(config, "artifact-123")

		Expect(err).To(MatchError(ContainSubstring("creating persistent temporary directory")))
	})

	newMountedConfig := func() *sdkConfig.Config {
		return withPersistent(GinkgoT().TempDir())
	}

	It("sends the node API key as a bearer header, and keeps it out of the URL", func() {
		var authHeader, requestURI string
		// Mirrors AuroraBoot's ArtifactImageMiddleware: an admin password is
		// honoured from either source, but a node API key counts only when it
		// arrives in the Authorization header.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			requestURI = r.URL.RequestURI()
			if authHeader != "Bearer node-api-key" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte("artifact image"))
		}))
		defer server.Close()

		tarPath, err := downloadArtifact(
			context.Background(), server.URL, "node-api-key", "artifact-123",
			newMountedConfig(), 0, time.Millisecond,
		)

		Expect(err).ToNot(HaveOccurred())
		defer func() { _ = os.Remove(tarPath) }()
		Expect(authHeader).To(Equal("Bearer node-api-key"))
		// No query string at all, so the key cannot reach an access log.
		Expect(requestURI).To(Equal("/api/v1/artifacts/artifact-123/image"))
		Expect(os.ReadFile(tarPath)).To(Equal([]byte("artifact image")))
	})

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
