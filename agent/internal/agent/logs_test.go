package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"path/filepath"

	"github.com/kairos-io/kairos-agent/v2/pkg/config"
	"github.com/kairos-io/kairos-agent/v2/pkg/constants"
	fsutils "github.com/kairos-io/kairos-agent/v2/pkg/utils/fs"
	v1mock "github.com/kairos-io/kairos-agent/v2/tests/mocks"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	sdkLogs "github.com/kairos-io/kairos-sdk/types/logs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

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
			// Default services: kairos-agent, kairos-installer, kairos-webui, cos-setup-boot, cos-setup-fs, cos-setup-network, cos-setup-reconcile, k3s, k3s-agent, k0scontroller, k0sworker
			// User service: myservice
			Expect(runner.CmdsMatch([][]string{
				{"journalctl", "-u", "kairos-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-installer", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-webui", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-boot", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-fs", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-network", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-reconcile", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k0scontroller", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k0sworker", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "myservice", "--no-pager", "-o", "cat"},
			})).To(BeNil())

			// Verify that both default and user-defined services are in the result
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).To(HaveKey("myservice"))
			Expect(result.Journal).To(HaveLen(12))
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
			// Default services: kairos-agent, kairos-installer, kairos-webui, cos-setup-boot, cos-setup-fs, cos-setup-network, cos-setup-reconcile, k3s, k3s-agent, k0scontroller, k0sworker
			// User service: myservice
			// Default files: /var/log/kairos-*.log (if exists)
			// User file: /var/log/test.log
			Expect(files).To(HaveKey("journal/kairos-agent.log"))
			Expect(files).To(HaveKey("journal/kairos-installer.log"))
			Expect(files).To(HaveKey("journal/kairos-webui.log"))
			Expect(files).To(HaveKey("journal/cos-setup-boot.log"))
			Expect(files).To(HaveKey("journal/cos-setup-fs.log"))
			Expect(files).To(HaveKey("journal/cos-setup-network.log"))
			Expect(files).To(HaveKey("journal/cos-setup-reconcile.log"))
			Expect(files).To(HaveKey("journal/k3s.log"))
			Expect(files).To(HaveKey("journal/k3s-agent.log"))
			Expect(files).To(HaveKey("journal/k0scontroller.log"))
			Expect(files).To(HaveKey("journal/k0sworker.log"))
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
			Expect(result.Journal).To(HaveLen(11))
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
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
			Expect(result.Journal).To(HaveLen(11))
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
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
			Expect(result.Journal).To(HaveLen(12))
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
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
			Expect(files).To(HaveKey("journal/kairos-agent.log"))
			Expect(files).To(HaveKey("journal/kairos-installer.log"))
			Expect(files).To(HaveKey("journal/kairos-webui.log"))
			Expect(files).To(HaveKey("journal/cos-setup-boot.log"))
			Expect(files).To(HaveKey("journal/cos-setup-fs.log"))
			Expect(files).To(HaveKey("journal/cos-setup-network.log"))
			Expect(files).To(HaveKey("journal/cos-setup-reconcile.log"))
			Expect(files).To(HaveKey("journal/k3s.log"))
			Expect(files).To(HaveKey("journal/k3s-agent.log"))
			Expect(files).To(HaveKey("journal/k0scontroller.log"))
			Expect(files).To(HaveKey("journal/k0sworker.log"))
			Expect(files).To(HaveKey("journal/existingservice.log"))
			Expect(files).ToNot(HaveKey("journal/nonexistentservice.log"))
			Expect(files).To(HaveLen(12))
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
			Expect(runner.CmdsMatch([][]string{
				{"journalctl", "-u", "kairos-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-installer", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-webui", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-boot", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-fs", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-network", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-reconcile", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k0scontroller", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k0sworker", "--no-pager", "-o", "cat"},
			})).To(BeNil())
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
			// Default services: kairos-agent, kairos-installer, kairos-webui, cos-setup-boot, cos-setup-fs, cos-setup-network, cos-setup-reconcile, k3s, k3s-agent, k0scontroller, k0sworker
			// User services: myservice, myotherservice
			Expect(runner.CmdsMatch([][]string{
				{"journalctl", "-u", "kairos-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-installer", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "kairos-webui", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-boot", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-fs", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-network", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "cos-setup-reconcile", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k3s-agent", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k0scontroller", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "k0sworker", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "myservice", "--no-pager", "-o", "cat"},
				{"journalctl", "-u", "myotherservice", "--no-pager", "-o", "cat"},
			})).To(BeNil())

			// Verify that both default and user-defined files are in the result
			Expect(result.Journal).To(HaveKey("kairos-agent"))
			Expect(result.Journal).To(HaveKey("kairos-installer"))
			Expect(result.Journal).To(HaveKey("kairos-webui"))
			Expect(result.Journal).To(HaveKey("cos-setup-boot"))
			Expect(result.Journal).To(HaveKey("cos-setup-fs"))
			Expect(result.Journal).To(HaveKey("cos-setup-network"))
			Expect(result.Journal).To(HaveKey("cos-setup-reconcile"))
			Expect(result.Journal).To(HaveKey("k3s"))
			Expect(result.Journal).To(HaveKey("k3s-agent"))
			Expect(result.Journal).To(HaveKey("k0scontroller"))
			Expect(result.Journal).To(HaveKey("k0sworker"))
			Expect(result.Journal).To(HaveKey("myservice"))
			Expect(result.Journal).To(HaveKey("myotherservice"))
			Expect(result.Journal).To(HaveLen(13))
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
