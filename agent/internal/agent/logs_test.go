package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos/v4/agent/tests/mocks"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	sdkLogs "github.com/kairos-io/kairos/v4/sdk/types/logs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// The specs below are about the merge with the user's config and the skipping
// of empty journals, not about the contents of the default journal list, so
// the helpers here derive that list rather than restate it. What the default
// list holds is asserted in logs_journal_units_test.go.

// defaultJournalUnits is what the collector asks journald for before the cloud
// config adds anything.
func defaultJournalUnits() []string {
	return defaultLogsConfig().Journal
}

// journalCmds is the journalctl invocation the collector makes for each
// default unit, in order, followed by the user-defined ones.
func journalCmds(userUnits ...string) [][]string {
	var cmds [][]string
	for _, unit := range append(defaultJournalUnits(), userUnits...) {
		cmds = append(cmds, []string{"journalctl", "-u", unit, "--no-pager", "-o", "cat"})
	}
	return cmds
}

// expectAllDefaults asserts every default unit made it into a collected set,
// keyed either by unit name (LogsResult.Journal) or by tarball entry.
func expectAllDefaults(keyed func(string) string, collected interface{}) {
	for _, unit := range defaultJournalUnits() {
		ExpectWithOffset(1, collected).To(HaveKey(keyed(unit)))
	}
}

func byUnit(unit string) string { return unit }

func byTarballEntry(unit string) string { return "journal/" + unit + ".log" }

var _ = Describe("Logs Command", Label("logs", "cmd"), func() {
	var (
		fs      *vfst.TestFS
		cleanup func()
		err     error
		logger  sdkLogger.KairosLogger
		runner  *v1mock.FakeRunner
	)

	// Helper function to create a mock systemctl file for systemd detection
	createMockSystemctl := func() {
		err := fsutils.MkdirAll(fs, "/usr/bin", constants.DirPerm)
		Expect(err).ToNot(HaveOccurred())
		err = fs.WriteFile("/usr/bin/systemctl", []byte("#!/bin/sh\necho 'mock systemctl'"), constants.FilePerm)
		Expect(err).ToNot(HaveOccurred())
	}

	BeforeEach(func() {
		fs, cleanup, err = vfst.NewTestFS(nil)
		Expect(err).ToNot(HaveOccurred())

		logger = sdkLogger.NewNullLogger()
		runner = v1mock.NewFakeRunner()

		// Create mock systemctl for systemd detection in all tests
		createMockSystemctl()
	})

	AfterEach(func() {
		cleanup()
	})

	Describe("LogsCollector", func() {
		var collector *LogsCollector

		BeforeEach(func() {
			cfg := config.NewConfig(
				config.WithFs(fs),
				config.WithLogger(logger),
				config.WithRunner(runner),
			)
			collector = NewLogsCollector(cfg)
		})

		It("should collect journal logs", func() {
			// Mock journalctl command output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 0 && args[0] == "-u" {
					service := args[1]
					return []byte("journal logs for " + service), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{"myservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify journalctl was called for both default and user-defined services
			Expect(runner.CmdsMatch(journalCmds("myservice"))).To(BeNil())

			// Verify that both default and user-defined services are in the result
			expectAllDefaults(byUnit, result.Journal)
			Expect(result.Journal).To(HaveKey("myservice"))
			Expect(result.Journal).To(HaveLen(len(defaultJournalUnits()) + 1))
		})

		It("should collect file logs with globbing", func() {
			// Create test files
			testFiles := []string{
				"/var/log/test1.log",
				"/var/log/test2.log",
				"/var/log/subdir/test3.log",
				"/var/log/kairos/agent.log", // Default pattern file
			}

			for _, file := range testFiles {
				err := fsutils.MkdirAll(fs, filepath.Dir(file), constants.DirPerm)
				Expect(err).ToNot(HaveOccurred())

				err = fs.WriteFile(file, []byte("log content for "+file), constants.FilePerm)
				Expect(err).ToNot(HaveOccurred())
			}

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/*.log", "/var/log/subdir/*.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify files were collected (both default and user-defined patterns)
			// Default pattern: /var/log/kairos/* (matches agent.log)
			// User patterns: /var/log/*.log, /var/log/subdir/*.log
			Expect(result.Files).To(HaveLen(4))
			Expect(result.Files).To(HaveKey("/var/log/test1.log"))
			Expect(result.Files).To(HaveKey("/var/log/test2.log"))
			Expect(result.Files).To(HaveKey("/var/log/subdir/test3.log"))
			Expect(result.Files).To(HaveKey("/var/log/kairos/agent.log"))
		})

		It("should handle nested directories correctly", func() {
			// Create test files with same basename in different directories
			testFiles := []string{
				"/var/log/test.log",
				"/var/log/subdir/test.log",  // Same basename, different directory
				"/var/log/kairos/agent.log", // Default pattern file
			}

			for _, file := range testFiles {
				err := fsutils.MkdirAll(fs, filepath.Dir(file), constants.DirPerm)
				Expect(err).ToNot(HaveOccurred())

				err = fs.WriteFile(file, []byte("log content for "+file), constants.FilePerm)
				Expect(err).ToNot(HaveOccurred())
			}

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/test.log", "/var/log/subdir/test.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify both files are collected with full paths as keys (including default pattern)
			Expect(result.Files).To(HaveKey("/var/log/test.log"))
			Expect(result.Files).To(HaveKey("/var/log/subdir/test.log"))
			Expect(result.Files).To(HaveKey("/var/log/kairos/agent.log"))
			Expect(result.Files).To(HaveLen(3))

			// Create tarball
			tarballPath := "/tmp/logs.tar.gz"
			err = collector.CreateTarball(result, tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Read and verify tarball contents
			tarballData, err := fs.ReadFile(tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Extract and verify tarball structure
			gzr, err := gzip.NewReader(bytes.NewReader(tarballData))
			Expect(err).ToNot(HaveOccurred())
			defer gzr.Close()

			tr := tar.NewReader(gzr)
			files := make(map[string]bool)

			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				files[header.Name] = true
			}

			// Verify that all files are preserved with their full directory structure
			Expect(files).To(HaveKey("files/var/log/test.log"))
			Expect(files).To(HaveKey("files/var/log/subdir/test.log"))
			Expect(files).To(HaveKey("files/var/log/kairos/agent.log"))
			Expect(files).To(HaveLen(3))
		})

		It("should create tarball with proper structure", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Create test file
			testFile := "/var/log/test.log"
			err := fsutils.MkdirAll(fs, filepath.Dir(testFile), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())

			err = fs.WriteFile(testFile, []byte("file log content"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{"myservice"},
				Files:   []string{testFile},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())

			// Create tarball
			tarballPath := "/tmp/logs.tar.gz"
			err = collector.CreateTarball(result, tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Verify tarball exists and has correct structure
			exists, err := fsutils.Exists(fs, tarballPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())

			// Read and verify tarball contents
			tarballData, err := fs.ReadFile(tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Extract and verify tarball structure
			gzr, err := gzip.NewReader(bytes.NewReader(tarballData))
			Expect(err).ToNot(HaveOccurred())
			defer gzr.Close()

			tr := tar.NewReader(gzr)
			files := make(map[string]bool)

			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				files[header.Name] = true
			}

			// Verify expected structure (both default and user-defined)
			// User service: myservice
			// Default files: /var/log/kairos-*.log (if exists)
			// User file: /var/log/test.log
			expectAllDefaults(byTarballEntry, files)
			Expect(files).To(HaveKey("journal/myservice.log"))
			Expect(files).To(HaveKey("files/var/log/test.log"))
		})

		It("should handle missing files gracefully", func() {
			// Create a default pattern file to ensure it's collected
			defaultFile := "/var/log/kairos/agent.log"
			err := fsutils.MkdirAll(fs, filepath.Dir(defaultFile), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())
			err = fs.WriteFile(defaultFile, []byte("default log content"), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{},
				Files:   []string{"/var/log/nonexistent.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected the default pattern file even though user file doesn't exist
			Expect(result.Files).To(HaveLen(1))
			Expect(result.Files).To(HaveKey("/var/log/kairos/agent.log"))
		})

		It("should handle journal service errors gracefully", func() {
			// Mock journalctl to return no entries for non-existent service
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 1 && args[1] == "nonexistentservice" {
					return []byte("-- No entries --"), nil
				}
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{"nonexistentservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected from default services but not the non-existent user service
			Expect(result.Journal).To(HaveLen(len(defaultJournalUnits())))
			expectAllDefaults(byUnit, result.Journal)
			Expect(result.Journal).ToNot(HaveKey("nonexistentservice"))
		})

		It("should handle empty journal output gracefully", func() {
			// Mock journalctl to return empty output for user service, normal for defaults
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 1 && args[1] == "emptyservice" {
					return []byte{}, nil
				}
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{"emptyservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected from default services but not the empty user service
			Expect(result.Journal).To(HaveLen(len(defaultJournalUnits())))
			expectAllDefaults(byUnit, result.Journal)
			Expect(result.Journal).ToNot(HaveKey("emptyservice"))
		})

		It("should not create tarball files for services with no journal entries", func() {
			// Mock journalctl to return no entries for one service and content for another
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 1 && args[1] == "existingservice" {
					return []byte("journal logs content"), nil
				}
				if command == "journalctl" && len(args) > 1 && args[1] == "nonexistentservice" {
					return []byte("-- No entries --"), nil
				}
				if command == "journalctl" {
					return []byte("default journal logs"), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{"existingservice", "nonexistentservice"},
				Files:   []string{},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			// Should have collected from default services and the existing user service
			Expect(result.Journal).To(HaveLen(len(defaultJournalUnits()) + 1))
			expectAllDefaults(byUnit, result.Journal)
			Expect(result.Journal).To(HaveKey("existingservice"))
			Expect(result.Journal).ToNot(HaveKey("nonexistentservice"))

			// Create tarball and verify contents
			tarballPath := "/tmp/logs.tar.gz"
			err = collector.CreateTarball(result, tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Read and verify tarball contents
			tarballData, err := fs.ReadFile(tarballPath)
			Expect(err).ToNot(HaveOccurred())

			// Extract and verify tarball structure
			gzr, err := gzip.NewReader(bytes.NewReader(tarballData))
			Expect(err).ToNot(HaveOccurred())
			defer gzr.Close()

			tr := tar.NewReader(gzr)
			files := make(map[string]bool)

			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				Expect(err).ToNot(HaveOccurred())
				files[header.Name] = true
			}

			// Verify that default services and existing user service files are created
			expectAllDefaults(byTarballEntry, files)
			Expect(files).To(HaveKey("journal/existingservice.log"))
			Expect(files).ToNot(HaveKey("journal/nonexistentservice.log"))
			Expect(files).To(HaveLen(len(defaultJournalUnits()) + 1))
		})

		It("should use default log sources when no config provided", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("default journal logs"), nil
				}
				return []byte{}, nil
			}

			// Don't set logs config - should use defaults
			collector.config.Logs = nil

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Should have collected from default services
			Expect(runner.CmdsMatch(journalCmds())).To(BeNil())
		})

		It("should merge user logs config with defaults", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" && len(args) > 0 && args[0] == "-u" {
					service := args[1]
					return []byte("journal logs for " + service), nil
				}
				return []byte{}, nil
			}

			// Set logs config in the main config with user-defined services
			collector.config.Logs = &sdkLogs.LogsConfig{
				Journal: []string{"myservice", "myotherservice"},
				Files:   []string{"/var/log/mycustom.log"},
			}

			result, err := collector.Collect()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())

			// Verify that both default and user-defined services were collected
			Expect(runner.CmdsMatch(journalCmds("myservice", "myotherservice"))).To(BeNil())

			// Verify that both default and user-defined files are in the result
			expectAllDefaults(byUnit, result.Journal)
			Expect(result.Journal).To(HaveKey("myservice"))
			Expect(result.Journal).To(HaveKey("myotherservice"))
			Expect(result.Journal).To(HaveLen(len(defaultJournalUnits()) + 2))
		})
	})

	Describe("CLI Command", func() {
		It("should execute logs command successfully", func() {
			// Mock journalctl output
			runner.SideEffect = func(command string, args ...string) ([]byte, error) {
				if command == "journalctl" {
					return []byte("journal logs content"), nil
				}
				return []byte{}, nil
			}

			// Create test config file
			configContent := `
logs:
  journal:
  - myservice
  files:
  - /var/log/test.log
`
			configPath := "/etc/kairos/config.yaml"
			err := fsutils.MkdirAll(fs, filepath.Dir(configPath), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())

			err = fs.WriteFile(configPath, []byte(configContent), constants.FilePerm)
			Expect(err).ToNot(HaveOccurred())

			// Create test log file
			testFile := "/var/log/test.log"
			err = fsutils.MkdirAll(fs, filepath.Dir(testFile), constants.DirPerm)
			Expect(err).ToNot(HaveOccurred())

			// Execute logs command
			outputPath := "/tmp/logs.tar.gz"
			err = ExecuteLogsCommand(fs, logger, runner, outputPath)
			Expect(err).ToNot(HaveOccurred())

			// Verify output file was created
			exists, err := fsutils.Exists(fs, outputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})

		It("should handle missing config gracefully", func() {
			// Execute logs command without config
			outputPath := "/tmp/logs.tar.gz"
			err := ExecuteLogsCommand(fs, logger, runner, outputPath)
			Expect(err).ToNot(HaveOccurred())

			// Should still create output with default sources
			exists, err := fsutils.Exists(fs, outputPath)
			Expect(err).ToNot(HaveOccurred())
			Expect(exists).To(BeTrue())
		})
	})
})
