package client

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kairos-io/kairos-challenger/pkg/attestation"
	"github.com/kairos-io/kairos-sdk/kcrypt/bus"
	"github.com/kairos-io/kairos-sdk/state"
	loggerpkg "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/kairos-io/kairos-sdk/types/partitions"
	"github.com/kairos-io/tpm-helpers"
	"github.com/mudler/go-pluggable"
)

const (
	NetworkRetryDelay = 1 * time.Second // delay for network/server issues
)

func NewClientWithLogger(logger loggerpkg.KairosLogger) (*Client, error) {
	// No config loading here - config is passed via the DiscoveryPasswordPayload JSON
	conf := newEmptyConfig()
	return &Client{Config: conf, Logger: logger}, nil
}

func (c *Client) Start(eventType pluggable.EventType) error {
	factory := pluggable.NewPluginFactory()

	factory.Add(bus.EventDiscoveryPassword, func(e *pluggable.Event) pluggable.EventResponse {
		payload := &bus.DiscoveryPasswordPayload{}
		err := json.Unmarshal([]byte(e.Data), payload)
		if err != nil {
			return pluggable.EventResponse{
				Error: fmt.Sprintf("failed reading payload: %s", err.Error()),
			}
		}

		// Log the received payload for debugging
		c.Logger.Debugf("Received payload: Data=%s, Partition=%v, ChallengerServer=%s, MDNS=%t",
			e.Data, payload.Partition != nil, payload.ChallengerServer, payload.MDNS)

		if payload.Partition == nil {
			return pluggable.EventResponse{
				Error: fmt.Sprintf("partition is required in payload. Received payload data: %s", e.Data),
			}
		}

		// Load config from collector first (for TPMDevice, etc.)
		// This ensures we have defaults even if payload doesn't provide them
		configFromCollector := LoadConfigFromCollector(c.Logger)

		// Apply config from collector to client
		c.Config = configFromCollector

		c.Logger.Debugf("Loaded config from collector: TPMDevice=%s, Server=%s, MDNS=%t",
			c.Config.Kcrypt.TPMDevice, c.Config.Kcrypt.Challenger.Server, c.Config.Kcrypt.Challenger.MDNS)

		// Apply config from payload (workload values take precedence)
		// Only override if payload provides values
		if payload.ChallengerServer != "" {
			c.Logger.Debugf("Using ChallengerServer from payload: %s", payload.ChallengerServer)
			c.Config.Kcrypt.Challenger.Server = payload.ChallengerServer
		}
		// Note: payload.MDNS is a bool, so we can't distinguish between "not set" and "false"
		// If payload has MDNS=true, use it. Otherwise, keep the collector's value.
		// The collector's value should already be set from LoadConfigFromCollector above.
		if payload.MDNS {
			c.Logger.Debugf("Using MDNS from payload: %t", payload.MDNS)
			c.Config.Kcrypt.Challenger.MDNS = payload.MDNS
		} else {
			c.Logger.Debugf("Payload MDNS not set or false, using collector value: %t", c.Config.Kcrypt.Challenger.MDNS)
		}

		c.Logger.Debugf("Final config: TPMDevice=%s, Server=%s, MDNS=%t",
			c.Config.Kcrypt.TPMDevice, c.Config.Kcrypt.Challenger.Server, c.Config.Kcrypt.Challenger.MDNS)

		pass, err := c.GetPassphrase(payload.Partition, 30)
		if err != nil {
			return pluggable.EventResponse{
				Error: fmt.Sprintf("failed getting pass: %s", err.Error()),
			}
		}

		return pluggable.EventResponse{
			Data: pass,
		}
	})

	return factory.Run(eventType, os.Stdin, os.Stdout)
}

// GetPassphrase retrieves a passphrase for the given partition - core business logic
// ❯ echo '{ "data": "{ \\"label\\": \\"LABEL\\" }"}' | sudo -E WSS_SERVER="http://localhost:8082/challenge" ./challenger "discovery.password"
func (c *Client) GetPassphrase(partition *partitions.Partition, attempts int) (string, error) {
	serverURL := c.Config.Kcrypt.Challenger.Server

	// Server details are required - local passphrase storage has been moved to kairos-sdk
	if serverURL == "" {
		return "", fmt.Errorf("challenger server URL is required (challenger_server must be configured or provided via payload)")
	}

	additionalHeaders := map[string]string{}
	var err error
	if c.Config.Kcrypt.Challenger.MDNS {
		c.Logger.Debugf("MDNS is enabled, attempting mDNS resolution for: %s", serverURL)
		serverURL, additionalHeaders, err = queryMDNS(serverURL, c.Logger)
		if err != nil {
			return "", fmt.Errorf("mDNS resolution failed: %w", err)
		}
		c.Logger.Debugf("mDNS resolution completed, using server URL: %s", serverURL)
	} else {
		c.Logger.Debugf("MDNS is disabled (MDNS=%t), skipping mDNS resolution", c.Config.Kcrypt.Challenger.MDNS)
	}

	c.Logger.Debugf("Starting TPM attestation flow with server: %s", serverURL)
	return c.waitPassWithTPMAttestation(serverURL, additionalHeaders, partition, attempts)
}

// attestationEndpointURL builds the TPM attestation endpoint from a challenger
// server URL, tolerating any number of trailing slashes on the input so that
// both "http://host/challenge" and "http://host/challenge/" work.
func attestationEndpointURL(serverURL string) string {
	return fmt.Sprintf("%s/tpm-attestation", strings.TrimRight(serverURL, "/"))
}

// waitPassWithTPMAttestation implements the new TPM remote attestation flow over WebSocket
func (c *Client) waitPassWithTPMAttestation(serverURL string, additionalHeaders map[string]string, p *partitions.Partition, attempts int) (string, error) {
	attestationEndpoint := attestationEndpointURL(serverURL)
	c.Logger.Debugf("Debug: TPM attestation endpoint: %s", attestationEndpoint)

	// Step 1: Initialize Remote Attestation Client (outside the retry loop)
	c.Logger.Debugf("Debug: Initializing Remote Attestation Client")
	clientOpts := []tpm.Option{}
	if c.Config.Kcrypt.TPMDevice != "" {
		c.Logger.Debugf("Debug: Using TPM device: %s", c.Config.Kcrypt.TPMDevice)
		clientOpts = append(clientOpts, tpm.WithTPMDevice(c.Config.Kcrypt.TPMDevice))
	}
	attestationClient, err := attestation.NewRemoteAttestationClient(clientOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to create attestation client: %w", err)
	}
	c.Logger.Debugf("Debug: Remote Attestation Client initialized successfully")

	// Ensure client is properly closed when done
	defer func() {
		if closeErr := attestationClient.Close(); closeErr != nil {
			c.Logger.Debugf("Warning: Failed to close attestation client: %v", closeErr)
		}
	}()

	var lastErr error
	for tries := range attempts {
		c.Logger.Debugf("Debug: TPM attestation attempt %d/%d", tries+1, attempts)

		// Step 2: Start WebSocket-based attestation flow
		c.Logger.Debugf("Debug: Starting WebSocket-based attestation flow")
		passphrase, err := c.performTPMAttestation(attestationEndpoint, additionalHeaders, attestationClient, p)
		if err != nil {
			c.Logger.Debugf("Failed TPM attestation: %v", err)
			lastErr = err

			// Don't retry on attestation failures (security rejections like quarantine, PCR mismatch, etc.)
			// These are permanent failures, not transient network issues
			if strings.Contains(err.Error(), "attestation failed") {
				c.Logger.Debugf("Attestation failure detected, not retrying")
				return "", err
			}

			time.Sleep(NetworkRetryDelay)
			continue
		}

		return passphrase, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("exhausted all attempts (%d) for TPM attestation, last error: %w", attempts, lastErr)
	}
	return "", fmt.Errorf("exhausted all attempts (%d) for TPM attestation", attempts)
}

// isLiveCDMode checks if the system is running in livecd mode
// using kairos-sdk state detection (same as `kairos-agent state get boot`)
func isLiveCDMode(logger loggerpkg.KairosLogger) bool {
	runtime, err := state.NewRuntimeWithLogger(logger.Logger)
	if err != nil {
		logger.Debugf("Failed to detect runtime state, assuming not livecd: %v", err)
		return false
	}

	// Check if boot state is LiveCD
	isLiveCD := runtime.BootState == state.LiveCD
	logger.Debugf("Detected boot state: %s (isLiveCD: %v)", runtime.BootState, isLiveCD)
	return isLiveCD
}

// performTPMAttestation handles the complete attestation flow over a single WebSocket connection
func (c *Client) performTPMAttestation(endpoint string, additionalHeaders map[string]string, attestationClient *attestation.RemoteAttestationClient, p *partitions.Partition) (string, error) {
	c.Logger.Debugf("Debug: Creating WebSocket connection to endpoint: %s", endpoint)
	c.Logger.Debugf("Debug: Partition details - Label: %s, Name: %s, UUID: %s", p.FilesystemLabel, p.Name, p.UUID)
	c.Logger.Debugf("Debug: Certificate length: %d", len(c.Config.Kcrypt.Challenger.Certificate))

	// Create WebSocket connection
	opts := []tpm.Option{
		tpm.WithAdditionalHeader("label", p.FilesystemLabel),
		tpm.WithAdditionalHeader("name", p.Name),
		tpm.WithAdditionalHeader("uuid", p.UUID),
	}

	// Only add certificate options if a certificate is provided
	if len(c.Config.Kcrypt.Challenger.Certificate) > 0 {
		c.Logger.Debugf("Debug: Adding certificate validation options")
		opts = append(opts,
			tpm.WithCAs([]byte(c.Config.Kcrypt.Challenger.Certificate)),
			tpm.AppendCustomCAToSystemCA,
		)
	} else {
		c.Logger.Debugf("Debug: No certificate provided, using insecure connection")
	}
	for k, v := range additionalHeaders {
		opts = append(opts, tpm.WithAdditionalHeader(k, v))
	}
	c.Logger.Debugf("Debug: WebSocket options configured, attempting connection...")

	// Add connection timeout to prevent hanging indefinitely
	type connectionResult struct {
		conn any
		err  error
	}

	done := make(chan connectionResult, 1)

	go func() {
		c.Logger.Debugf("Debug: Using tpm.AttestationConnection for new TPM flow")
		conn, err := tpm.AttestationConnection(endpoint, opts...)
		c.Logger.Debugf("Debug: tpm.AttestationConnection returned with err: %v", err)
		done <- connectionResult{conn: conn, err: err}
	}()

	var conn *websocket.Conn
	select {
	case result := <-done:
		if result.err != nil {
			c.Logger.Debugf("Debug: WebSocket connection failed: %v", result.err)
			return "", fmt.Errorf("creating WebSocket connection: %w", result.err)
		}
		var ok bool
		conn, ok = result.conn.(*websocket.Conn)
		if !ok {
			return "", fmt.Errorf("unexpected connection type")
		}
		c.Logger.Debugf("Debug: WebSocket connection established successfully")
	case <-time.After(10 * time.Second):
		c.Logger.Debugf("Debug: WebSocket connection timed out after 10 seconds")
		return "", fmt.Errorf("WebSocket connection timed out")
	}

	defer conn.Close() //nolint:errcheck

	// Protocol Step 1: Create and send attestation init
	c.Logger.Debugf("Debug: Creating attestation init")

	// Check if we're in livecd mode - if so, defer PCR enrollment
	var init *attestation.AttestationInit
	var err error
	if isLiveCDMode(c.Logger) {
		c.Logger.Debugf("Debug: LiveCD mode detected - deferring PCR enrollment")
		init, err = attestationClient.CreateInitDeferredEnrollment()
	} else {
		init, err = attestationClient.CreateInit()
	}
	if err != nil {
		return "", fmt.Errorf("creating attestation init: %w", err)
	}

	c.Logger.Debugf("Debug: Sending attestation init to server")
	if err := conn.WriteJSON(init); err != nil {
		return "", fmt.Errorf("sending attestation init: %w", err)
	}
	c.Logger.Debugf("Debug: Attestation init sent successfully")

	// Protocol Step 2: Receive challenge from server
	c.Logger.Debugf("Debug: Waiting for challenge from server")
	var challenge attestation.AttestationChallenge
	if err := conn.ReadJSON(&challenge); err != nil {
		return "", fmt.Errorf("reading challenge from server: %w", err)
	}
	c.Logger.Debugf("Challenge received")

	// Protocol Step 3: Handle challenge and create proof
	c.Logger.Debugf("Debug: Handling challenge")
	// Use default PCRs for now - this could be made configurable
	pcrs := []int{0, 7, 11} // Common PCRs used in the system
	proof, err := attestationClient.HandleChallenge(&challenge, pcrs)
	if err != nil {
		c.Logger.Debugf("Debug: HandleChallenge failed: %v", err)
		return "", fmt.Errorf("handling challenge: %w", err)
	}
	c.Logger.Debugf("Debug: Challenge handled successfully")

	// Protocol Step 4: Send proof to server
	c.Logger.Debugf("Debug: Sending proof to server")
	if err := conn.WriteJSON(proof); err != nil {
		return "", fmt.Errorf("sending proof: %w", err)
	}
	c.Logger.Debugf("Proof sent")

	// Protocol Step 5: Receive passphrase response from server
	c.Logger.Debugf("Debug: Waiting for passphrase response")
	var response attestation.AttestationResponse
	if err := conn.ReadJSON(&response); err != nil {
		return "", fmt.Errorf("reading passphrase response: %w", err)
	}
	c.Logger.Debugf("Response received")

	// Check if the server returned an error
	if response.Error != "" {
		c.Logger.Debugf("Server returned error: %s", response.Error)
		return "", fmt.Errorf("attestation failed: %s", response.Error)
	}

	// Check if we received an empty passphrase (shouldn't happen if no error, but defensive check)
	if len(response.Passphrase) == 0 {
		return "", fmt.Errorf("server returned empty passphrase without error message")
	}

	c.Logger.Debugf("Passphrase received successfully")
	return string(response.Passphrase), nil
}
