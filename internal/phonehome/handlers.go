package phonehome

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kairos-io/kairos-agent/v2/pkg/action"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
)

var selectBootEntry = action.SelectBootEntry
var rebootScheduler = scheduleReboot
var persistentDir = constants.PersistentDir

func createArtifactTempFile(config *sdkConfig.Config, artifactID string) (*os.File, error) {
	if config == nil || config.Mounter == nil {
		return nil, fmt.Errorf("cannot verify the persistent partition mount")
	}
	notMounted, err := config.Mounter.IsLikelyNotMountPoint(persistentDir)
	if err != nil {
		return nil, fmt.Errorf("checking persistent partition mount: %w", err)
	}
	if notMounted {
		return nil, fmt.Errorf("persistent partition %s is not mounted", persistentDir)
	}

	tempDir := filepath.Join(persistentDir, "tmp")
	if err := os.MkdirAll(tempDir, 0750); err != nil {
		return nil, fmt.Errorf("creating persistent temporary directory: %w", err)
	}
	return os.CreateTemp(tempDir, fmt.Sprintf("phonehome-upgrade-%s-*.tar", artifactID))
}

// DefaultCommandHandler returns a CommandHandler that handles all phone-home commands.
// The serverURL and apiKey are needed to download artifact images for upgrades.
//
// isAllowed gates execution: a command is only dispatched if isAllowed(cmd.Command)
// returns true. This exists because a rogue/DNS-hijacked server could otherwise
// drive arbitrary `exec`, `reset`, or `apply-cloud-config` on the node. Destructive
// commands are opt-in per Config.AllowedCommands. If isAllowed is nil, every command
// is denied (safer default than allowing everything when the caller forgets to wire
// the policy through).
//
// stop is invoked after a successful `unregister` teardown so the long-lived
// phonehome Run loop stops reconnecting. It is nil-safe (nil => no self-exit,
// useful for tests and one-shot handler drivers); in production the Client
// passes its own Stop method in.
func DefaultCommandHandler(serverURL string, apiKey func() string, isAllowed func(string) bool, stop func(), systemConfig *sdkConfig.Config, phonehomeConfig ...*Config) CommandHandler {
	retries, retryInterval := artifactDownloadRetrySettings(nil)
	if len(phonehomeConfig) > 0 {
		retries, retryInterval = artifactDownloadRetrySettings(phonehomeConfig[0])
	}

	return func(cmd CommandData) (string, error) {
		if isAllowed == nil || !isAllowed(cmd.Command) {
			return "", fmt.Errorf("command %q is not permitted by the phonehome policy; add it to phonehome.allowed_commands in cloud-config to opt in", cmd.Command)
		}

		ctx := context.Background()

		switch cmd.Command {
		case "exec":
			cmdStr, ok := cmd.Args["command"]
			if !ok {
				return "", fmt.Errorf("exec command requires 'command' arg")
			}
			// Arbitrary shell is opt-in via phonehome.allowed_commands (see gate above).
			out, err := exec.CommandContext(ctx, "sh", "-c", cmdStr).CombinedOutput() //nosec G204 -- gated by Config.AllowedCommands policy
			return string(out), err

		case "upgrade", "upgrade-recovery":
			return handleUpgrade(ctx, cmd, serverURL, apiKey(), systemConfig, retries, retryInterval)

		case "reset":
			return handleReset(cmd, systemConfig)

		case "apply-cloud-config":
			return handleApplyCloudConfig(cmd)

		case "reboot":
			return handleReboot()

		case "unregister":
			return handleUnregister(stop)

		default:
			return "", fmt.Errorf("unknown command: %s", cmd.Command)
		}
	}
}

func artifactDownloadRetrySettings(config *Config) (int, time.Duration) {
	if config == nil {
		return DefaultArtifactDownloadRetries, DefaultArtifactDownloadRetryInterval
	}
	retries := config.ArtifactDownloadRetries
	if retries <= 0 {
		retries = DefaultArtifactDownloadRetries
	}
	interval := config.ArtifactDownloadRetryInterval
	if interval <= 0 {
		interval = DefaultArtifactDownloadRetryInterval
	}
	if interval > MaxArtifactDownloadRetryInterval {
		interval = MaxArtifactDownloadRetryInterval
	}
	return retries, interval
}

// handleUnregister runs the on-host phone-home teardown from inside the
// running service and then schedules the Run loop to exit.
//
// Crucially, this passes stopService=false to Uninstall: the handler IS the
// service process, so `systemctl stop kairos-agent-phonehome` would SIGTERM
// it mid-call and the "Completed" WebSocket status would never be written.
// Instead we remove the unit file, daemon-reload, clean up the state on
// disk, and then after the summary is sent the client stops itself via
// Client.Stop(). The 500 ms delay gives the Completed write time to flush
// through the WS and the TCP buffers before we tear down the client.
func handleUnregister(stop func()) (string, error) {
	summary, err := Uninstall(false)
	if stop != nil {
		time.AfterFunc(500*time.Millisecond, stop)
	}
	if err != nil {
		return summary, err
	}
	return summary, nil
}

// handleUpgrade downloads the image (if artifact-based) and runs kairos-agent upgrade.
func handleUpgrade(ctx context.Context, cmd CommandData, serverURL string, apiKey string, systemConfig *sdkConfig.Config, retries int, retryInterval time.Duration) (string, error) {
	source := cmd.Args["source"]
	if source == "" {
		return "", fmt.Errorf("upgrade requires 'source' arg")
	}

	// If source is "artifact:<id>", download the container image tar from the server.
	if strings.HasPrefix(source, "artifact:") {
		artifactID := strings.TrimPrefix(source, "artifact:")
		// Artifact IDs come from the management server — constrain to a safe
		// character set so they can't poison the temporary filename or URL path.
		if !isSafeArtifactID(artifactID) {
			return "", fmt.Errorf("invalid artifact id %q", artifactID)
		}

		tarPath, err := downloadArtifact(ctx, serverURL, apiKey, artifactID, systemConfig, retries, retryInterval)
		if err != nil {
			return "", err
		}
		defer func() { _ = os.Remove(tarPath) }()

		source = "ocifile:" + tarPath
	} else if !strings.Contains(source, ":") {
		source = "oci:" + source
	}

	args := []string{"upgrade", "--source", source}
	if cmd.Command == "upgrade-recovery" || cmd.Args["recovery"] == "true" {
		args = append(args, "--recovery")
	}

	// Use background context — upgrade must NOT be killed if WS disconnects
	Logger.Infof("running: kairos-agent %s", strings.Join(args, " "))
	out, err := exec.Command("kairos-agent", args...).CombinedOutput() //nosec G204 -- args is a fixed set built from validated CommandData fields
	if err != nil {
		Logger.Errorf("kairos-agent upgrade exit: err=%v output=%s", err, string(out))
		return string(out), err
	}
	Logger.Infof("kairos-agent upgrade completed: %s", string(out))

	// Reboot after successful upgrade so the new image takes effect.
	// Do NOT reboot for recovery upgrades (recovery doesn't need reboot).
	if cmd.Command != "upgrade-recovery" {
		scheduleReboot()
	}

	return string(out) + "\nUpgrade complete. Rebooting in 10s...", nil
}

func downloadArtifact(ctx context.Context, serverURL, apiKey, artifactID string, systemConfig *sdkConfig.Config, retries int, retryInterval time.Duration) (string, error) {
	imageURL := fmt.Sprintf("%s/api/v1/artifacts/%s/image?token=%s",
		strings.TrimRight(serverURL, "/"), artifactID, apiKey)
	attempts := retries + 1
	if attempts < 1 {
		attempts = 1
	}

	var finalErr error
	backoff := retryInterval
	for attempt := 1; attempt <= attempts; attempt++ {
		tarPath, transient, err := downloadArtifactAttempt(ctx, imageURL, artifactID, systemConfig)
		if err == nil {
			return tarPath, nil
		}
		finalErr = err
		if !transient {
			return "", fmt.Errorf("downloading artifact image: %w", err)
		}
		if attempt == attempts {
			break
		}

		Logger.Warnf("artifact download attempt %d/%d failed: %v; retrying in %s", attempt, attempts, err, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return "", fmt.Errorf("downloading artifact image: %w", ctx.Err())
		case <-timer.C:
		}
		if backoff >= MaxArtifactDownloadRetryInterval/2 {
			backoff = MaxArtifactDownloadRetryInterval
		} else {
			backoff *= 2
		}
	}

	return "", fmt.Errorf("downloading artifact image after %d attempts: %w", attempts, finalErr)
}

func downloadArtifactAttempt(ctx context.Context, imageURL, artifactID string, systemConfig *sdkConfig.Config) (string, bool, error) {
	// serverURL is operator-configured via cloud-config, not user input.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil) //nosec G107 -- URL derived from operator cloud-config
	if err != nil {
		return "", false, fmt.Errorf("building request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		var networkErr *url.Error
		if errors.As(err, &networkErr) {
			err = networkErr.Err
		}
		return "", true, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode >= http.StatusInternalServerError, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := createArtifactTempFile(systemConfig, artifactID)
	if err != nil {
		return "", false, fmt.Errorf("creating tar file: %w", err)
	}
	tarPath := f.Name()
	if _, err = io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tarPath)
		return "", isTransientDownloadError(err), fmt.Errorf("writing tar file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tarPath)
		return "", false, fmt.Errorf("closing tar file: %w", err)
	}
	return tarPath, false, nil
}

func isTransientDownloadError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

// handleReset selects the automatic state-reset boot entry and reboots. Reset
// itself cannot run from the active system; the statereset entry performs it on
// the next boot and then returns the node to the active entry.
func handleReset(cmd CommandData, systemConfig *sdkConfig.Config) (string, error) {
	for _, argument := range []string{"reset-oem", "config"} {
		if _, ok := cmd.Args[argument]; ok {
			return "", fmt.Errorf("reset argument %q is not supported by automatic state reset", argument)
		}
	}
	if systemConfig == nil {
		return "", fmt.Errorf("reset requires the scanned system configuration")
	}
	if err := selectBootEntry(systemConfig, constants.StateResetImgName); err != nil {
		return "", fmt.Errorf("selecting automatic state-reset boot entry: %w", err)
	}

	rebootScheduler()
	return "Automatic state reset selected. Rebooting in 10s...", nil
}

// handleApplyCloudConfig writes a cloud-config file to the OEM partition.
func handleApplyCloudConfig(cmd CommandData) (string, error) {
	cfg := cmd.Args["config"]
	if cfg == "" {
		return "", fmt.Errorf("apply-cloud-config requires 'config' arg")
	}

	if err := writeOEMCloudConfig(cfg); err != nil {
		return "", err
	}

	return "Cloud config written to /oem/99_phonehome_remote.yaml. Reboot to apply.", nil
}

// handleReboot schedules a system reboot.
func handleReboot() (string, error) {
	scheduleReboot()
	return "Rebooting in 10s...", nil
}

// writeOEMCloudConfig ensures OEM is mounted and writes a cloud-config file.
func writeOEMCloudConfig(content string) error {
	// Ensure #cloud-config header
	if !strings.HasPrefix(strings.TrimSpace(content), "#cloud-config") {
		content = "#cloud-config\n" + content
	}

	// Ensure /oem is mounted (it may have been unmounted during reset).
	// MkdirAll is best-effort: if /oem already exists we proceed; any other
	// failure will surface from the mount attempt or WriteFile below.
	if err := os.MkdirAll("/oem", 0750); err != nil {
		Logger.Warnf("mkdir /oem: %v", err)
	}
	// Best-effort mount — error is expected and ignored when /oem is already mounted.
	_ = exec.Command("mount", "-L", "COS_OEM", "/oem").Run() //nosec G204 -- fixed label, called on local mountpoint

	return os.WriteFile("/oem/99_phonehome_remote.yaml", []byte(content), 0600)
}

// scheduleReboot syncs filesystems and reboots after a short delay.
func scheduleReboot() {
	go func() {
		// Best-effort flush then reboot — we're going down either way.
		_ = exec.Command("sync").Run() //nosec G204 -- fixed command
		time.Sleep(10 * time.Second)
		_ = exec.Command("reboot").Run() //nosec G204 -- fixed command
	}()
}

// isSafeArtifactID whitelists characters acceptable inside an artifact
// identifier: alphanumeric, dash, underscore, dot. This prevents the ID from
// containing path separators or shell metacharacters when it is interpolated
// into /tmp paths and request URLs.
func isSafeArtifactID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
