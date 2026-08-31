package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	"github.com/kairos-io/provider-kairos/v2/internal/provider/assets"

	"github.com/kairos-io/kairos/v4/sdk/machine"
	"github.com/kairos-io/kairos/v4/sdk/machine/systemd"
	"github.com/kairos-io/kairos/v4/sdk/utils"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	"github.com/kairos-io/provider-kairos/v2/internal/services"
)

const (
	enabledValue                = "true"
	DefaultEdgeVPNAPIAddress    = "unix:///run/edgevpn-kairos.sock"
	DefaultEdgeVPNAPISocketMode = "0600"

	// EdgeVPNEnvFile is where SetupAPI and SetupVPN record the edgevpn
	// daemon's settings for its systemd unit. It is the only place that
	// knows which address the daemon was actually told to listen on.
	EdgeVPNEnvFile = "/etc/systemd/system.conf.d/edgevpn-kairos.env"
)

// ResolveAPIAddress returns the address a client should use to reach the local
// edgevpn API, taken from the APILISTEN the daemon was configured with.
//
// The daemon's address is decided at bootstrap time from whatever the agent
// passes, so it can differ from this package's default. A client that assumes
// the default then talks to an address nothing is listening on, and because the
// API client's errors are easy to drop, that looks like an empty answer rather
// than a failure. Reading back what the daemon was given keeps both ends on one
// address instead of two that have to agree by coincidence.
//
// Falls back to DefaultEdgeVPNAPIAddress when the file is missing or carries no
// APILISTEN, which is the state of a node that has not bootstrapped yet.
func ResolveAPIAddress(envFile string) string {
	env, err := godotenv.Read(envFile)
	if err != nil {
		return DefaultEdgeVPNAPIAddress
	}

	listen := strings.TrimSpace(env["APILISTEN"])
	if listen == "" {
		return DefaultEdgeVPNAPIAddress
	}

	return clientAddressForListener(listen)
}

// clientAddressForListener turns an APILISTEN value into something the API
// client can dial. normalizeAPIAddress strips the scheme before the value is
// written, but the client builds request URLs by concatenating host and path,
// so a bare host:port has to get its scheme back or the URL will not parse.
// Unix socket addresses keep their unix:// prefix, which the client looks for
// to swap in a socket transport.
func clientAddressForListener(listen string) string {
	switch {
	case strings.HasPrefix(listen, "unix://"),
		strings.HasPrefix(listen, "http://"),
		strings.HasPrefix(listen, "https://"):
		return listen
	default:
		return "http://" + listen
	}
}

// edgeVPNEnvDirMode is the mode for the drop-in directory holding the daemon's
// env file. It is a shared system directory, so it takes the usual 0755 rather
// than anything tighter; the secrets live in the file, not in the listing.
const edgeVPNEnvDirMode = 0755

// writeEdgeVPNEnv records the edgevpn daemon's settings where its systemd unit
// will read them, creating the drop-in directory if it is not there yet.
//
// Both the directory and the file hang off rootDir, so a build writing into a
// staging root does not reach into the host's /etc.
func writeEdgeVPNEnv(rootDir string, opts map[string]string) error {
	envFile := filepath.Join(rootDir, EdgeVPNEnvFile)
	envDir := filepath.Dir(envFile)

	if err := os.MkdirAll(envDir, edgeVPNEnvDirMode); err != nil {
		return fmt.Errorf("could not create %s: %w", envDir, err)
	}

	if err := utils.WriteEnv(envFile, opts); err != nil {
		return fmt.Errorf("could not write %s: %w", envFile, err)
	}

	return nil
}

func normalizeAPIAddress(apiAddress string) string {
	if apiAddress == "" {
		apiAddress = DefaultEdgeVPNAPIAddress
	}
	apiAddress = strings.ReplaceAll(apiAddress, "https://", "")
	apiAddress = strings.ReplaceAll(apiAddress, "http://", "")
	return apiAddress
}

func applyAPIListenerEnv(opts, userEnv map[string]string) {
	for k, v := range userEnv {
		opts[k] = v
	}
	if _, explicitlySet := userEnv["APILISTENUNIXMODE"]; !explicitlySet &&
		strings.HasPrefix(opts["APILISTEN"], "unix://") {
		opts["APILISTENUNIXMODE"] = DefaultEdgeVPNAPISocketMode
	}
}

func SaveCloudConfig(name string, c []byte) error {
	return os.WriteFile(filepath.Join("oem", fmt.Sprintf("%s.yaml", name)), c, 0700)
}

func SetupAPI(apiAddress, rootDir string, start bool, c *providerConfig.Config) error {
	if c.P2P == nil || c.P2P.NetworkToken == "" {
		return fmt.Errorf("no network token defined")
	}

	svc, err := services.P2PAPI(rootDir)
	if err != nil {
		return fmt.Errorf("could not create svc: %w", err)
	}

	vpnOpts := map[string]string{
		"EDGEVPNTOKEN": c.P2P.NetworkToken,
		"APILISTEN":    normalizeAPIAddress(apiAddress),
	}
	applyAPIListenerEnv(vpnOpts, c.P2P.VPN.Env)

	if c.P2P.DisableDHT {
		vpnOpts["EDGEVPNDHT"] = "false"
	}

	// Setup edgevpn instance
	if err := writeEdgeVPNEnv(rootDir, vpnOpts); err != nil {
		return err
	}

	err = svc.WriteUnit()
	if err != nil {
		return fmt.Errorf("could not create write unit file: %w", err)
	}

	if start {
		err = svc.Start()
		if err != nil {
			return fmt.Errorf("could not start svc: %w", err)
		}

		return svc.Enable()
	}
	return nil
}

func SetupVPN(instance, apiAddress, rootDir string, start bool, c *providerConfig.Config) error {
	token := ""
	if c.P2P != nil && c.P2P.NetworkToken != "" {
		token = c.P2P.NetworkToken
	}

	svc, err := services.EdgeVPN(instance, rootDir)
	if err != nil {
		return fmt.Errorf("could not create svc: %w", err)
	}

	vpnOpts := map[string]string{
		"API":          enabledValue,
		"APILISTEN":    normalizeAPIAddress(apiAddress),
		"DHCP":         enabledValue,
		"DHCPLEASEDIR": "/usr/local/.kairos/lease",
	}
	if token != "" {
		vpnOpts["EDGEVPNTOKEN"] = c.P2P.NetworkToken
	}

	if c.P2P.DisableDHT {
		vpnOpts["EDGEVPNDHT"] = "false"
	}

	applyAPIListenerEnv(vpnOpts, c.P2P.VPN.Env)

	if c.P2P.DNS {
		vpnOpts["DNSADDRESS"] = "127.0.0.1:53"
		vpnOpts["DNSFORWARD"] = enabledValue

		_ = machine.ExecuteInlineCloudConfig(assets.LocalDNS, "initramfs")
		if !utils.IsOpenRCBased() {
			svc, err := systemd.NewService(
				systemd.WithName("systemd-resolved"),
			)
			if err == nil {
				_ = svc.Restart()
			}
		}

		if err := SaveCloudConfig("vpn_dns", []byte(assets.LocalDNS)); err != nil {
			return fmt.Errorf("could not create dns config: %w", err)
		}
	}

	// Setup edgevpn instance
	if err := writeEdgeVPNEnv(rootDir, vpnOpts); err != nil {
		return err
	}

	err = svc.WriteUnit()
	if err != nil {
		return fmt.Errorf("could not create write unit file: %w", err)
	}

	if start {
		err = svc.Start()
		if err != nil {
			return fmt.Errorf("could not start svc: %w", err)
		}

		return svc.Enable()
	}
	return nil
}
