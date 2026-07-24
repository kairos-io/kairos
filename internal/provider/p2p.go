package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/provider-kairos/v2/internal/provider/assets"

	"github.com/kairos-io/kairos-sdk/machine"
	"github.com/kairos-io/kairos-sdk/machine/systemd"
	"github.com/kairos-io/kairos-sdk/utils"
	providerConfig "github.com/kairos-io/provider-kairos/v2/internal/provider/config"
	"github.com/kairos-io/provider-kairos/v2/internal/services"
)

const (
	enabledValue                = "true"
	DefaultEdgeVPNAPIAddress    = "unix:///run/edgevpn-kairos.sock"
	DefaultEdgeVPNAPISocketMode = "0600"
)

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

	os.MkdirAll("/etc/systemd/system.conf.d/", 0600) //nolint:errcheck
	// Setup edgevpn instance
	err = utils.WriteEnv(filepath.Join(rootDir, "/etc/systemd/system.conf.d/edgevpn-kairos.env"), vpnOpts)
	if err != nil {
		return fmt.Errorf("could not create write env file: %w", err)
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

	os.MkdirAll("/etc/systemd/system.conf.d/", 0600) //nolint:errcheck
	// Setup edgevpn instance
	err = utils.WriteEnv(filepath.Join(rootDir, "/etc/systemd/system.conf.d/edgevpn-kairos.env"), vpnOpts)
	if err != nil {
		return fmt.Errorf("could not create write env file: %w", err)
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
