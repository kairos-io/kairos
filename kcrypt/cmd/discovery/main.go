package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kairos-io/kairos-challenger/cmd/discovery/client"
	attpkg "github.com/kairos-io/kairos-challenger/pkg/attestation"
	"github.com/kairos-io/kairos-sdk/kcrypt/bus"
	loggerpkg "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/kairos-io/tpm-helpers"
	"github.com/mudler/go-pluggable"
	"github.com/spf13/cobra"
)

// GetFlags holds all flags specific to the get command
type GetFlags struct {
	PartitionName     string
	PartitionUUID     string
	PartitionLabel    string
	Attempts          int
	ChallengerServer  string
	EnableMDNS        bool
	ServerCertificate string
	TPMDevice         string
}

var (
	// Global/persistent flags
	debug bool

	// Shared TPM device flag (for root and info commands)
	sharedTPMDevice string
)

// rootCmd represents the base command (TPM hash generation)
var rootCmd = &cobra.Command{
	Use:   "kcrypt-discovery-challenger",
	Short: "kcrypt-challenger discovery client",
	Long: `kcrypt-challenger discovery client

This tool provides TPM-based operations for encrypted partition management.
By default, it outputs the TPM hash for this device.

Configuration:
  The client reads configuration from Kairos configuration files in the following directories:
  - /oem (during installation from ISO)
  - /sysroot/oem (on installed systems during initramfs)
  - /tmp/oem (when running in hooks)

  Configuration format (YAML):
    kcrypt:
      challenger:
        challenger_server: "https://my-server.com:8082"  # Server URL
        mdns: true                                       # Enable mDNS discovery
        certificate: "/path/to/server-cert.pem"         # Server certificate
        tpm_device: "/dev/tpmrm0"                        # TPM device path`,
	Example: `  # Get TPM hash for this device (default)
  kcrypt-discovery-challenger

  # Get passphrase for encrypted partition
  kcrypt-discovery-challenger get --partition-name=/dev/sda2

  # Display TPM enrollment information (hash, PCRs, EK)
  kcrypt-discovery-challenger info

  # Clean up TPM NV memory (useful for development)
  kcrypt-discovery-challenger cleanup

  # Run plugin event
  kcrypt-discovery-challenger discovery.password`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTPMHash()
	},
}

// newInfoCmd creates the info command
func newInfoCmd() *cobra.Command {
	var pcrs string
	var tpmDevice string

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display TPM enrollment information",
		Long: `Display TPM information used for remote KMS enrollment.

This command shows the TPM hash, PCR values, and EK public key that are
sent to the remote KMS during enrollment. This is useful for debugging
and comparing client-side values with what's stored on the server.

The --pcrs flag accepts a comma-separated list of PCR indices to display.
If not specified, defaults to PCRs 0,7,11 (the standard set used for enrollment).`,
		Example: `  # Display info with default PCRs (0,7,11)
  kcrypt-discovery-challenger info

  # Display info with specific PCRs
  kcrypt-discovery-challenger info --pcrs=0,1,2,7

  # Display info with all PCRs
  kcrypt-discovery-challenger info --pcrs=0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo(pcrs, tpmDevice)
		},
	}

	cmd.Flags().StringVar(&pcrs, "pcrs", "0,7,11", "Comma-separated list of PCR indices to display")

	// Add shared TPM device flag
	addSharedTPMDeviceFlag(cmd, &tpmDevice)

	return cmd
}

// newGetCmd creates the get command with its flags
func newGetCmd() *cobra.Command {
	flags := &GetFlags{}

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get passphrase for encrypted partition",
		Long: `Get passphrase for encrypted partition using TPM attestation.

This command retrieves passphrases for encrypted partitions by communicating
with a challenger server using TPM-based attestation. At least one partition
identifier (name, UUID, or label) must be provided.

The command uses configuration from the root command's config files, but flags
can override specific settings:
  --challenger-server  Override kcrypt.challenger.challenger_server
  --mdns               Override kcrypt.challenger.mdns
  --certificate        Override kcrypt.challenger.certificate`,
		Example: `  # Get passphrase using partition name
  kcrypt-discovery-challenger get --partition-name=/dev/sda2

  # Get passphrase using UUID
  kcrypt-discovery-challenger get --partition-uuid=12345-abcde

  # Get passphrase using filesystem label
  kcrypt-discovery-challenger get --partition-label=encrypted-data

  # Get passphrase with multiple identifiers
  kcrypt-discovery-challenger get --partition-name=/dev/sda2 --partition-uuid=12345-abcde --partition-label=encrypted-data

  # Get passphrase with custom server
  kcrypt-discovery-challenger get --partition-label=encrypted-data --challenger-server=https://my-server.com:8082`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate that at least one partition identifier is provided
			if flags.PartitionName == "" && flags.PartitionUUID == "" && flags.PartitionLabel == "" {
				return fmt.Errorf("at least one of --partition-name, --partition-uuid, or --partition-label must be provided")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGetPassphrase(flags)
		},
	}

	// Register flags
	cmd.Flags().StringVar(&flags.PartitionName, "partition-name", "", "Name of the partition (at least one identifier required)")
	cmd.Flags().StringVar(&flags.PartitionUUID, "partition-uuid", "", "UUID of the partition (at least one identifier required)")
	cmd.Flags().StringVar(&flags.PartitionLabel, "partition-label", "", "Filesystem label of the partition (at least one identifier required)")
	cmd.Flags().IntVar(&flags.Attempts, "attempts", 30, "Number of attempts to get the passphrase")
	cmd.Flags().StringVar(&flags.ChallengerServer, "challenger-server", "", "URL of the challenger server (overrides config)")
	cmd.Flags().BoolVar(&flags.EnableMDNS, "mdns", false, "Enable mDNS discovery (overrides config)")
	cmd.Flags().StringVar(&flags.ServerCertificate, "certificate", "", "Server certificate for verification (overrides config)")

	// Add shared TPM device flag
	addSharedTPMDeviceFlag(cmd, &flags.TPMDevice)

	return cmd
}

// pluginCmd represents the plugin event commands
var pluginCmd = &cobra.Command{
	Use:   string(bus.EventDiscoveryPassword),
	Short: fmt.Sprintf("Run %s plugin event", bus.EventDiscoveryPassword),
	Long: fmt.Sprintf(`Run the %s plugin event.

This command runs in plugin mode, reading JSON partition data from stdin
and outputting the passphrase to stdout. This is used for integration
with kcrypt and other tools.`, bus.EventDiscoveryPassword),
	Example: fmt.Sprintf(`  # Plugin mode (for integration with kcrypt)
  echo '{"data": "{\"name\": \"/dev/sda2\", \"uuid\": \"12345-abcde\", \"label\": \"encrypted-data\"}"}' | kcrypt-discovery-challenger %s`, bus.EventDiscoveryPassword),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPluginMode(bus.EventDiscoveryPassword)
	},
}

// addSharedTPMDeviceFlag adds the shared TPM device flag to a command
func addSharedTPMDeviceFlag(cmd *cobra.Command, tpmDevice *string) {
	cmd.Flags().StringVar(tpmDevice, "tpm-device", "", "TPM device path (overrides config)")
}

func init() {
	// Global/persistent flags (available to all commands)
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug logging")

	// Add shared TPM device flag to root command
	addSharedTPMDeviceFlag(rootCmd, &sharedTPMDevice)

	// Add subcommands
	rootCmd.AddCommand(newGetCmd())
	rootCmd.AddCommand(newInfoCmd())
	rootCmd.AddCommand(pluginCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ExecuteWithArgs executes the root command with the given arguments.
// This function is used by tests to simulate CLI execution.
func ExecuteWithArgs(args []string) error {
	// Set command arguments (this overrides os.Args)
	rootCmd.SetArgs(args)

	return rootCmd.Execute()
}

// loadConfigWithOverrides loads config from collector and applies CLI flag overrides
func loadConfigWithOverrides(tpmDevice string, logger loggerpkg.KairosLogger) client.Config {
	// Load config from collector
	conf := client.LoadConfigFromCollector(logger)

	// Apply CLI overrides if provided
	if tpmDevice != "" {
		logger.Debugf("Overriding TPM device from CLI: %s", tpmDevice)
		conf.Kcrypt.TPMDevice = tpmDevice
	} else {
		logger.Debugf("No TPM device override from CLI, using config value: %s", conf.Kcrypt.TPMDevice)
	}

	return conf
}

// runTPMHash handles the root command - TPM hash generation
func runTPMHash() error {
	// Create logger based on debug flag
	var logger loggerpkg.KairosLogger
	if debug {
		logger = loggerpkg.NewKairosLogger("kcrypt-discovery-challenger", "debug", false)
		logger.Debugf("Debug mode enabled for TPM hash generation")
	} else {
		logger = loggerpkg.NewKairosLogger("kcrypt-discovery-challenger", "error", false)
	}

	// Load configuration with CLI overrides
	config := loadConfigWithOverrides(sharedTPMDevice, logger)

	// Initialize AK Manager for transient AK approach
	logger.Debugf("Initializing AK Manager for transient AK approach")
	akManagerOpts := []tpm.Option{}
	if config.Kcrypt.TPMDevice != "" {
		logger.Debugf("Using TPM device: %s", config.Kcrypt.TPMDevice)
		akManagerOpts = append(akManagerOpts, tpm.WithTPMDevice(config.Kcrypt.TPMDevice))
	} else {
		logger.Debugf("No TPM device specified, using default")
	}
	akManager, err := tpm.NewAKManager(akManagerOpts...)
	if err != nil {
		return fmt.Errorf("creating AK manager: %w", err)
	}
	logger.Debugf("AK Manager initialized successfully")

	// Get EK for attestation (transient AK approach)
	logger.Debugf("Getting EK for attestation")
	ek, err := akManager.GetEK()
	if err != nil {
		return fmt.Errorf("getting EK: %w", err)
	}
	logger.Debugf("Attestation data retrieved successfully")

	// Compute TPM hash from EK using attestation helper (SHA-256 of EK SPKI)
	logger.Debugf("Computing TPM hash from EK")
	tpmHash, err := attpkg.ComputeTPMHashFromEK(ek)
	if err != nil {
		return fmt.Errorf("computing TPM hash: %w", err)
	}
	logger.Debugf("TPM hash computed successfully: %s", tpmHash)

	// Output the TPM hash to stdout
	fmt.Print(tpmHash)
	return nil
}

// runGetPassphrase handles the get subcommand - passphrase retrieval
func runGetPassphrase(flags *GetFlags) error {
	// Create logger based on debug flag
	var logger loggerpkg.KairosLogger
	if debug {
		logger = loggerpkg.NewKairosLogger("kcrypt-discovery-challenger", "debug", false)
	} else {
		logger = loggerpkg.NewKairosLogger("kcrypt-discovery-challenger", "error", false)
	}

	// Create client with potential CLI overrides
	c, err := createClientWithOverrides(flags.ChallengerServer, flags.EnableMDNS, flags.ServerCertificate, flags.TPMDevice, logger)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	// Create partition object
	partition := &partitions.Partition{
		Name:            flags.PartitionName,
		UUID:            flags.PartitionUUID,
		FilesystemLabel: flags.PartitionLabel,
	}

	// Log partition information
	logger.Debugf("Partition details:")
	logger.Debugf("  Name: %s", partition.Name)
	logger.Debugf("  UUID: %s", partition.UUID)
	logger.Debugf("  Label: %s", partition.FilesystemLabel)
	logger.Debugf("  Attempts: %d", flags.Attempts)

	passphrase, err := c.GetPassphrase(partition, flags.Attempts)
	if err != nil {
		return fmt.Errorf("getting passphrase: %w", err)
	}

	// WARNING: The following line outputs sensitive information (the passphrase) to stdout.
	// Ensure this is only used in secure / ephemeral contexts (e.g., piped directly to the consumer).
	fmt.Print(passphrase)
	return nil
}

// runPluginMode handles plugin event commands
func runPluginMode(eventType pluggable.EventType) error {
	// In plugin mode, use quiet=true to log to file instead of console
	// Log level depends on debug flag, write logs to /var/log/kairos/kcrypt-discovery-challenger.log
	var logLevel string
	if debug {
		logLevel = "debug"
	} else {
		logLevel = "error"
	}

	logger := loggerpkg.NewKairosLoggerWithExtraDirs("kcrypt-discovery-challenger", logLevel, true, "/var/log/kairos")
	logger.Debugf("Debug mode enabled for plugin mode")
	c, err := client.NewClientWithLogger(logger)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	err = c.Start(eventType)
	if err != nil {
		return fmt.Errorf("starting plugin: %w", err)
	}
	return nil
}

// createClientWithOverrides creates a client and applies CLI flag overrides to the config
func createClientWithOverrides(serverURL string, enableMDNS bool, certificate string, tpmDevice string, logger loggerpkg.KairosLogger) (*client.Client, error) {
	// Start with config loaded from collector with TPM overrides
	conf := loadConfigWithOverrides(tpmDevice, logger)

	// Create client with the loaded config
	c, err := client.NewClientWithLogger(logger)
	if err != nil {
		return nil, err
	}

	// Apply the loaded config
	c.Config = conf

	// Log the original configuration values
	logger.Debugf("Original configuration:")
	logger.Debugf("  Server: %s", c.Config.Kcrypt.Challenger.Server)
	logger.Debugf("  MDNS: %t", c.Config.Kcrypt.Challenger.MDNS)
	logger.Debugf("  Certificate: %s", maskSensitiveString(c.Config.Kcrypt.Challenger.Certificate))
	logger.Debugf("  TPMDevice: %s", c.Config.Kcrypt.TPMDevice)

	// Apply CLI overrides if provided
	if serverURL != "" {
		logger.Debugf("Overriding server URL: %s -> %s", c.Config.Kcrypt.Challenger.Server, serverURL)
		c.Config.Kcrypt.Challenger.Server = serverURL
	}

	// For boolean flags, we can directly use the value since Cobra handles it properly
	if enableMDNS {
		logger.Debugf("Overriding MDNS setting: %t -> %t", c.Config.Kcrypt.Challenger.MDNS, enableMDNS)
		c.Config.Kcrypt.Challenger.MDNS = enableMDNS
	}

	if certificate != "" {
		logger.Debugf("Overriding certificate: %s -> %s",
			maskSensitiveString(c.Config.Kcrypt.Challenger.Certificate),
			maskSensitiveString(certificate))
		c.Config.Kcrypt.Challenger.Certificate = certificate
	}

	// Log the final configuration values
	logger.Debugf("Final configuration:")
	logger.Debugf("  Server: %s", c.Config.Kcrypt.Challenger.Server)
	logger.Debugf("  MDNS: %t", c.Config.Kcrypt.Challenger.MDNS)
	logger.Debugf("  Certificate: %s", maskSensitiveString(c.Config.Kcrypt.Challenger.Certificate))
	logger.Debugf("  TPMDevice: %s", c.Config.Kcrypt.TPMDevice)

	return c, nil
}

// runInfo handles the info subcommand - display TPM enrollment information
func runInfo(pcrsFlag string, tpmDevice string) error {
	// Create logger based on debug flag
	var logger loggerpkg.KairosLogger
	if debug {
		logger = loggerpkg.NewKairosLogger("kcrypt-discovery-challenger", "debug", false)
		logger.Debugf("Debug mode enabled for info command")
	} else {
		logger = loggerpkg.NewKairosLogger("kcrypt-discovery-challenger", "error", false)
	}

	// Parse PCR indices from comma-separated string
	pcrIndices, err := parsePCRList(pcrsFlag)
	if err != nil {
		return fmt.Errorf("parsing PCR list: %w", err)
	}
	logger.Debugf("Displaying info for PCRs: %v", pcrIndices)

	// Load configuration with CLI overrides
	config := loadConfigWithOverrides(tpmDevice, logger)

	// Initialize AK Manager
	logger.Debugf("Initializing AK Manager")
	akManagerOpts := []tpm.Option{}
	if config.Kcrypt.TPMDevice != "" {
		logger.Debugf("Using TPM device: %s", config.Kcrypt.TPMDevice)
		akManagerOpts = append(akManagerOpts, tpm.WithTPMDevice(config.Kcrypt.TPMDevice))
	}
	akManager, err := tpm.NewAKManager(akManagerOpts...)
	if err != nil {
		return fmt.Errorf("creating AK manager: %w", err)
	}
	defer akManager.Close()
	logger.Debugf("AK Manager initialized successfully")

	// Get EK for attestation
	logger.Debugf("Getting EK for attestation")
	ek, err := akManager.GetEK()
	if err != nil {
		return fmt.Errorf("getting EK: %w", err)
	}

	// Compute TPM hash from EK
	logger.Debugf("Computing TPM hash from EK")
	tpmHash, err := attpkg.ComputeTPMHashFromEK(ek)
	if err != nil {
		return fmt.Errorf("computing TPM hash: %w", err)
	}

	// Get EK public key in PEM format
	logger.Debugf("Encoding EK to PEM")
	ekPEM, err := attpkg.EncodeEKToPEM(ek)
	if err != nil {
		return fmt.Errorf("encoding EK to PEM: %w", err)
	}

	// Read PCR values
	logger.Debugf("Reading PCR values")
	pcrQuote, err := akManager.GeneratePCRQuote(pcrIndices)
	if err != nil {
		return fmt.Errorf("reading PCR values: %w", err)
	}

	// Parse the PCR quote to extract PCR values
	var quoteData struct {
		Quote struct {
			Version   string `json:"version"`
			Quote     []byte `json:"quote"`
			Signature []byte `json:"signature"`
		} `json:"quote"`
		PCRs map[int][]byte `json:"pcrs"`
	}
	if err := json.Unmarshal(pcrQuote, &quoteData); err != nil {
		return fmt.Errorf("parsing PCR quote: %w", err)
	}

	// Display the information
	fmt.Printf("TPM Enrollment Information\n")
	fmt.Printf("==========================\n\n")

	fmt.Printf("TPM Hash:\n")
	fmt.Printf("  %s\n\n", tpmHash)

	fmt.Printf("PCR Values:\n")
	for _, idx := range pcrIndices {
		if pcrValue, exists := quoteData.PCRs[idx]; exists {
			fmt.Printf("  PCR %2d: %x\n", idx, pcrValue)
		} else {
			fmt.Printf("  PCR %2d: <not available>\n", idx)
		}
	}
	fmt.Printf("\n")

	fmt.Printf("EK Public Key (PEM):\n")
	fmt.Printf("%s\n", ekPEM)

	return nil
}

// parsePCRList parses a comma-separated string of PCR indices
// Returns a unique, sorted list of PCR indices
func parsePCRList(pcrsStr string) ([]int, error) {
	if pcrsStr == "" {
		return nil, fmt.Errorf("PCR list cannot be empty")
	}

	parts := strings.Split(pcrsStr, ",")
	pcrSet := make(map[int]bool) // Use map to ensure uniqueness

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var pcrIndex int
		if _, err := fmt.Sscanf(part, "%d", &pcrIndex); err != nil {
			return nil, fmt.Errorf("invalid PCR index '%s': %w", part, err)
		}

		if pcrIndex < 0 || pcrIndex > 23 {
			return nil, fmt.Errorf("PCR index %d is out of range (0-23)", pcrIndex)
		}

		pcrSet[pcrIndex] = true
	}

	if len(pcrSet) == 0 {
		return nil, fmt.Errorf("no valid PCR indices found")
	}

	// Convert map to sorted slice
	pcrIndices := make([]int, 0, len(pcrSet))
	for pcrIndex := range pcrSet {
		pcrIndices = append(pcrIndices, pcrIndex)
	}
	sort.Ints(pcrIndices)

	return pcrIndices, nil
}

// maskSensitiveString masks certificate paths/content for logging
func maskSensitiveString(s string) string {
	if s == "" {
		return "<empty>"
	}
	if len(s) <= 10 {
		return strings.Repeat("*", len(s))
	}
	// Show first 3 and last 3 characters with * in between
	return s[:3] + strings.Repeat("*", len(s)-6) + s[len(s)-3:]
}
