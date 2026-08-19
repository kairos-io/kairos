package agent

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	"github.com/kairos-io/kairos-sdk/collector"
	sdkConfig "github.com/kairos-io/kairos-sdk/types/config"
	sdkFS "github.com/kairos-io/kairos-sdk/types/fs"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkLogs "github.com/kairos-io/kairos-sdk/types/logs"
	sdkRunner "github.com/kairos-io/kairos-sdk/types/runner"
)

// LogsResult represents the collected logs
type LogsResult struct {
	Journal map[string][]byte `yaml:"-"`
	Files   map[string][]byte `yaml:"-"`
}

// LogsCollector handles the collection of logs from various sources
type LogsCollector struct {
	config *sdkConfig.Config
}

// NewLogsCollector creates a new LogsCollector instance
func NewLogsCollector(cfg *sdkConfig.Config) *LogsCollector {
	return &LogsCollector{
		config: cfg,
	}
}

func defaultLogsConfig() *sdkLogs.LogsConfig {
	return &sdkLogs.LogsConfig{
		Journal: []string{
			"kairos-agent",
			"kairos-installer",
			"kairos-webui",
			"cos-setup-boot",
			"cos-setup-fs",
			"cos-setup-network",
			"cos-setup-reconcile",
			"k3s",
			"k3s-agent",
			"k0scontroller",
			"k0sworker",
		},
		Files: []string{
			"/var/log/kairos/*.log",
			"/var/log/*.log",
			"/run/immucore/*.log",
		},
	}
}

// isSystemdAvailable checks if systemd is available on the system
func (lc *LogsCollector) isSystemdAvailable() bool {
	// Check for systemctl in common locations
	for _, path := range []string{"/sbin/systemctl", "/bin/systemctl", "/usr/sbin/systemctl", "/usr/bin/systemctl"} {
		if _, err := lc.config.Fs.Stat(path); err == nil {
			lc.config.Logger.Debugf("Found systemd at %s", path)
			return true
		}
	}
	lc.config.Logger.Debugf("systemd not found, skipping journal log collection")
	return false
}

// Collect gathers logs based on the configuration stored in the LogsCollector
func (lc *LogsCollector) Collect() (*LogsResult, error) {
	result := &LogsResult{
		Journal: make(map[string][]byte),
		Files:   make(map[string][]byte),
	}

	// Define default configuration
	logsConfig := defaultLogsConfig()

	// Merge user configuration with defaults
	if lc.config.Logs != nil {
		logsConfig.Journal = append(logsConfig.Journal, lc.config.Logs.Journal...)
		logsConfig.Files = append(logsConfig.Files, lc.config.Logs.Files...)
	}

	// Check if systemd is available before collecting journal logs
	if lc.isSystemdAvailable() {
		// Collect journal logs
		for _, service := range logsConfig.Journal {
			output, err := lc.config.Runner.Run("journalctl", "-u", service, "--no-pager", "-o", "cat")
			if err != nil {
				lc.config.Logger.Warnf("Failed to collect journal logs for service %s: %v", service, err)
				continue
			}

			// Skip services with no journal entries
			if len(output) == 0 || string(output) == "-- No entries --" {
				lc.config.Logger.Debugf("No journal entries found for service %s, skipping", service)
				continue
			}

			result.Journal[service] = output
		}
	} else {
		lc.config.Logger.Infof("systemd not available, skipping journal log collection")
	}

	// Collect file logs with globbing support
	for _, pattern := range logsConfig.Files {
		matches, err := lc.globFiles(pattern)
		if err != nil {
			lc.config.Logger.Warnf("Failed to glob pattern %s: %v", pattern, err)
			continue
		}

		for _, file := range matches {
			content, err := lc.config.Fs.ReadFile(file)
			if err != nil {
				lc.config.Logger.Warnf("Failed to read file %s: %v", file, err)
				continue
			}
			result.Files[file] = content
		}
	}

	return result, nil
}

// CreateTarball creates a compressed tarball from the collected logs
func (lc *LogsCollector) CreateTarball(result *LogsResult, outputPath string) error {
	// Create output directory if it doesn't exist
	outputDir := filepath.Dir(outputPath)
	if err := fsutils.MkdirAll(lc.config.Fs, outputDir, constants.DirPerm); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create the tarball file
	file, err := lc.config.Fs.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create tarball file: %w", err)
	}
	defer file.Close()

	// Create gzip writer
	gw := gzip.NewWriter(file)
	defer gw.Close()

	// Create tar writer
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Add journal logs to tarball
	for service, content := range result.Journal {
		header := &tar.Header{
			Name: fmt.Sprintf("journal/%s.log", service),
			Mode: constants.FilePerm,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write journal header: %w", err)
		}
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write journal content: %w", err)
		}
	}

	// Add file logs to tarball
	for filePath, content := range result.Files {
		// Remove leading slash and use full path structure
		relativePath := strings.TrimPrefix(filePath, "/")
		header := &tar.Header{
			Name: fmt.Sprintf("files/%s", relativePath),
			Mode: constants.FilePerm,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write file header: %w", err)
		}
		if _, err := tw.Write(content); err != nil {
			return fmt.Errorf("failed to write file content: %w", err)
		}
	}

	return nil
}

// globFiles expands glob patterns to matching files using the standard library
func (lc *LogsCollector) globFiles(pattern string) ([]string, error) {
	matches, err := fs.Glob(lc.config.Fs, pattern)
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// ExecuteLogsCommand executes the logs command with the given parameters
func ExecuteLogsCommand(fs sdkFS.KairosFS, logger sdkLogger.KairosLogger, runner sdkRunner.Runner, outputPath string) error {
	// Scan for configuration from default locations
	cfg, err := config.Scan(collector.Directories(constants.GetUserConfigDirs()...), collector.NoLogs)
	if err != nil {
		logger.Warnf("Failed to load configuration, using defaults: %v", err)
		// Create a minimal config with just the required components
		cfg = config.NewConfig(
			config.WithFs(fs),
			config.WithLogger(logger),
			config.WithRunner(runner),
		)
	} else {
		// Update the scanned config with the provided components
		cfg.Fs = fs
		cfg.Logger = logger
		cfg.Runner = runner
	}

	collector := NewLogsCollector(cfg)

	result, err := collector.Collect()
	if err != nil {
		return fmt.Errorf("failed to collect logs: %w", err)
	}

	if err := collector.CreateTarball(result, outputPath); err != nil {
		return fmt.Errorf("failed to create tarball: %w", err)
	}

	logger.Infof("Logs collected successfully to %s", outputPath)
	return nil
}
