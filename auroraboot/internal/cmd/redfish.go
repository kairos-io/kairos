package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/hardware"
	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/urfave/cli/v2"
)

// redfishPasswordEnv is the environment variable consulted for the RedFish
// password when neither --password nor --password-file is set.
const redfishPasswordEnv = "AURORABOOT_REDFISH_PASSWORD"

// redfishQuirksDirEnv is the environment variable consulted for the operator
// quirk-profile directory when --quirks-dir is not set.
const redfishQuirksDirEnv = "AURORABOOT_REDFISH_QUIRKS_DIR"

// loadRedfishQuirksDir loads operator-supplied quirk profiles from dir (if any)
// and installs the resulting registry as the process-wide default so subsequent
// redfish.NewDeployer calls resolve --vendor against the loaded profiles. It is
// load-at-start: profiles are read once here, never hot-reloaded mid-deploy. Each
// loaded profile and each malformed-and-skipped file is logged by the registry; a
// single bad profile disables only itself and never fails the command. An empty
// dir is a no-op (built-in profiles only). Only a directory-read failure is fatal.
func loadRedfishQuirksDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = os.Getenv(redfishQuirksDirEnv)
	}
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	registry, _, err := redfish.LoadProfileDir(dir)
	if err != nil {
		return fmt.Errorf("loading quirk profiles: %w", err)
	}
	redfish.SetDefaultRegistry(registry)
	return nil
}

// resolveRedfishPassword resolves the RedFish password from, in precedence order:
// the explicit --password flag, --password-file, the AURORABOOT_REDFISH_PASSWORD
// environment variable, then --password-stdin. It errors if none is provided.
// stdin is injected for testability.
func resolveRedfishPassword(flagPassword, passwordFile string, passwordStdin bool, stdin io.Reader) (string, error) {
	switch {
	case flagPassword != "":
		return flagPassword, nil
	case passwordFile != "":
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("reading --password-file: %w", err)
		}
		pw := strings.TrimRight(string(data), "\r\n")
		if pw == "" {
			return "", fmt.Errorf("--password-file %q is empty", passwordFile)
		}
		return pw, nil
	case os.Getenv(redfishPasswordEnv) != "":
		return os.Getenv(redfishPasswordEnv), nil
	case passwordStdin:
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		pw := strings.TrimRight(string(data), "\r\n")
		if pw == "" {
			return "", fmt.Errorf("no password read from stdin")
		}
		return pw, nil
	default:
		return "", fmt.Errorf("no RedFish password provided: set --password (insecure), --password-file, %s, or --password-stdin", redfishPasswordEnv)
	}
}

// resolveBootMode validates the operator-supplied --boot-mode flag and maps it to
// a redfish.BootMode. The empty string is valid and intentional: it means "leave
// the system in its current firmware mode" — the Deployer then omits
// BootSourceOverrideMode from the boot PATCH, which avoids forcing a firmware-mode
// change that some BMCs/emulators reject. The accepted explicit values are
// "uefi" and "legacy" (case-insensitive).
func resolveBootMode(flag string) (redfish.BootMode, error) {
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "":
		return "", nil
	case "uefi":
		return redfish.BootModeUEFI, nil
	case "legacy":
		return redfish.BootModeLegacy, nil
	default:
		return "", fmt.Errorf("invalid --boot-mode %q (valid: uefi, legacy, or empty to leave the current firmware mode)", flag)
	}
}

// deriveServeBindAddr derives the local ISO-serve bind address from the
// advertised --redfish-serve-url when --redfish-serve-addr is not set explicitly.
// It requires the URL to carry both a host and an explicit port: a missing port
// makes net.Listen fail with a cryptic error, so reject it with an actionable
// message rather than silently picking one.
func deriveServeBindAddr(serveURL string) (string, error) {
	parsed, err := url.Parse(serveURL)
	if err != nil {
		return "", fmt.Errorf("parsing --redfish-serve-url: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("--redfish-serve-url has no host; set --redfish-serve-addr explicitly")
	}
	if parsed.Port() == "" {
		return "", fmt.Errorf("could not derive a bind address from --redfish-serve-url %q (no port); set --redfish-serve-addr explicitly, e.g. %s:8090", serveURL, parsed.Hostname())
	}
	return parsed.Host, nil
}

// validateServeTLSFlags checks the local ISO server's TLS posture is internally
// consistent before anything starts listening. --serve-tls is the single switch:
// it needs a certificate pair, the pair does nothing without it, and the URL
// advertised to the BMC has to carry the scheme the server actually speaks. A
// disagreement is always an operator error, never a working deployment, because
// the BMC fetches the advertised URL verbatim.
func validateServeTLSFlags(serveTLS bool, certFile, keyFile, serveURL string) error {
	if serveTLS && (certFile == "" || keyFile == "") {
		return fmt.Errorf("--serve-tls with a local ISO requires --serve-tls-cert and --serve-tls-key (the BMC fetches over HTTPS)")
	}
	if !serveTLS && (certFile != "" || keyFile != "") {
		return fmt.Errorf("--serve-tls-cert/--serve-tls-key without --serve-tls: pass --serve-tls to serve the local ISO over HTTPS, or drop the certificate flags")
	}

	urlHTTPS, err := isoserve.URLUsesHTTPS(serveURL)
	if err != nil {
		return fmt.Errorf("reading --redfish-serve-url scheme: %w", err)
	}
	switch {
	case serveTLS && !urlHTTPS:
		return fmt.Errorf("--serve-tls serves the local ISO over HTTPS but --redfish-serve-url %q is not https; the BMC would be told to fetch the wrong scheme", serveURL)
	case !serveTLS && urlHTTPS:
		return fmt.Errorf("--redfish-serve-url %q is https but the local ISO server has no certificate; pass --serve-tls with --serve-tls-cert and --serve-tls-key", serveURL)
	}
	return nil
}

var RedFishDeployCmd = cli.Command{
	Name:  "redfish",
	Usage: "Deploy ISO to server via RedFish (EXPERIMENTAL)",
	Subcommands: []*cli.Command{
		redfishProbeCmd(),
		{
			Name:  "deploy",
			Usage: "Deploy ISO to server via RedFish",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "endpoint",
					Usage:    "RedFish endpoint URL",
					Required: true,
				},
				&cli.StringFlag{
					Name:     "username",
					Usage:    "RedFish username",
					Required: true,
				},
				&cli.StringFlag{
					Name:  "password",
					Usage: "RedFish password (INSECURE: visible in the process list/argv; prefer --password-file, AURORABOOT_REDFISH_PASSWORD, or --password-stdin)",
				},
				&cli.StringFlag{
					Name:  "password-file",
					Usage: "Read the RedFish password from this file (trailing newline trimmed)",
				},
				&cli.BoolFlag{
					Name:  "password-stdin",
					Usage: "Read the RedFish password from standard input (trailing newline trimmed)",
				},
				&cli.StringFlag{
					Name:  "image-url",
					Usage: "URL the BMC pulls the ISO from (InsertMedia is URL-pull; the BMC must be able to reach this URL)",
				},
				&cli.StringFlag{
					Name:  "system-id",
					Usage: "Redfish ComputerSystem Id to target; required when the BMC exposes more than one system",
				},
				&cli.StringFlag{
					Name:  "vendor",
					Usage: "Quirk profile to select: a built-in vendor (generic, supermicro, ilo, dmtf) OR the name of an operator profile loaded from --quirks-dir. An unknown name falls back to generic",
					Value: "generic",
				},
				&cli.StringFlag{
					Name:    "quirks-dir",
					Usage:   "Directory of operator-supplied *.yaml/*.yml quirk profiles loaded at start. Selected by name via --vendor; a profile named the same as a built-in overrides it (logged). A malformed profile is skipped, not fatal",
					EnvVars: []string{redfishQuirksDirEnv},
				},
				&cli.StringFlag{
					Name:  "boot-mode",
					Usage: "Force the one-time boot firmware mode: uefi or legacy. Omit (the default) to leave the system in its current mode and NOT send a boot-mode change — recommended for most BMCs, since forcing a mode makes some BMCs/emulators reconfigure the firmware and fail",
				},
				&cli.StringFlag{
					Name:  "auth-mode",
					Usage: "Redfish authentication mode: session (deletable session, requires a SessionService), basic (HTTP Basic; credentials sent on every request, no session), or auto (detect from the ServiceRoot and fall back to basic when no SessionService is advertised)",
					Value: "auto",
				},
				&cli.BoolFlag{
					Name:  "verify-ssl",
					Usage: "Verify SSL certificates",
					Value: true,
				},
				&cli.BoolFlag{
					Name:  "serve-tls",
					Usage: "Serve the local ISO over HTTPS; requires --serve-tls-cert, --serve-tls-key and an https:// --redfish-serve-url. Not valid with --image-url (the transfer protocol follows its scheme)",
					Value: false,
				},
				&cli.StringFlag{
					Name:  "serve-tls-cert",
					Usage: "TLS certificate file for the local ISO-serve (used with --serve-tls)",
				},
				&cli.StringFlag{
					Name:  "serve-tls-key",
					Usage: "TLS key file for the local ISO-serve (used with --serve-tls)",
				},
				&cli.StringFlag{
					Name:  "redfish-serve-url",
					Usage: "Advertised base URL the BMC fetches a local ISO from (e.g. http://10.0.0.5:8090). Required when deploying a local ISO path without --image-url",
				},
				&cli.StringFlag{
					Name:  "redfish-serve-addr",
					Usage: "Bind address for the local ISO-serve (e.g. 10.0.0.5:8090). Defaults to the host:port of --redfish-serve-url",
				},
				&cli.IntFlag{
					Name:  "min-memory",
					Usage: "Minimum required memory in GiB",
					Value: 4,
				},
				&cli.IntFlag{
					Name:  "min-cpus",
					Usage: "Minimum required CPUs",
					Value: 2,
				},
				&cli.StringSliceFlag{
					Name:  "required-features",
					Usage: "Hardware features the system must support; the deploy aborts if any is missing or cannot be verified. Detectable features: UEFI, SecureBoot (default: UEFI)",
					Value: cli.NewStringSlice("UEFI"),
				},
				&cli.DurationFlag{
					Name:  "timeout",
					Usage: "Overall operation timeout, counted from connecting to the BMC; a deploy whose task is still running when it runs out fails",
					Value: 30 * time.Minute,
				},
			},
			Action: func(c *cli.Context) error {
				ctx := c.Context

				// Load operator quirk profiles (if any) BEFORE NewDeployer so --vendor
				// can select one by name. Load-at-start, not hot-reload.
				if err := loadRedfishQuirksDir(c.String("quirks-dir")); err != nil {
					return err
				}

				endpoint := c.String("endpoint")
				username := c.String("username")
				password, err := resolveRedfishPassword(
					c.String("password"),
					c.String("password-file"),
					c.Bool("password-stdin"),
					os.Stdin,
				)
				if err != nil {
					return err
				}
				imageURL := c.String("image-url")
				systemID := c.String("system-id")
				vendor := c.String("vendor")
				bootMode, err := resolveBootMode(c.String("boot-mode"))
				if err != nil {
					return err
				}
				authMode := c.String("auth-mode")
				verifySSL := c.Bool("verify-ssl")
				serveTLS := c.Bool("serve-tls")
				serveTLSCert := c.String("serve-tls-cert")
				serveTLSKey := c.String("serve-tls-key")
				serveURL := c.String("redfish-serve-url")
				serveAddr := c.String("redfish-serve-addr")
				minMemory := c.Int("min-memory")
				minCPUs := c.Int("min-cpus")
				requiredFeatures := c.StringSlice("required-features")
				timeout := c.Duration("timeout")
				isoPath := c.Args().First()

				// SSRF-guard the operator-supplied BMC endpoint regardless of mode.
				if err := isoserve.ValidateMediaURL(endpoint); err != nil {
					return fmt.Errorf("validating endpoint: %w", err)
				}

				// Resolve the image URL. InsertMedia is URL-pull, so the BMC needs a
				// URL it can fetch. Two modes:
				//   1. --image-url given: SSRF-validate and use it directly.
				//   2. a local ISO path given: start a one-shot tokenized ISO-serve,
				//      Register the file, deploy with the returned URL, then
				//      Revoke/Shutdown after the deploy returns.
				var (
					serve      *isoserve.Server
					serveToken string
					useHTTPS   bool
				)
				switch {
				case imageURL != "":
					if err := isoserve.ValidateMediaURL(imageURL); err != nil {
						return fmt.Errorf("validating --image-url: %w", err)
					}
					if serveTLS || serveTLSCert != "" || serveTLSKey != "" {
						return fmt.Errorf("--serve-tls and --serve-tls-cert/--serve-tls-key configure the local ISO server, which --image-url bypasses; the transfer protocol follows the scheme of --image-url")
					}
					// The BMC fetches the operator's own URL, so the advertised
					// TransferProtocolType has to follow that URL's scheme.
					useHTTPS, err = isoserve.URLUsesHTTPS(imageURL)
					if err != nil {
						return fmt.Errorf("validating --image-url: %w", err)
					}
				case isoPath != "":
					absISO, err := filepath.Abs(isoPath)
					if err != nil {
						return fmt.Errorf("resolving ISO path: %w", err)
					}
					if serveURL == "" {
						return fmt.Errorf("serving a local ISO requires --redfish-serve-url (the base URL the BMC fetches the ISO from)")
					}
					if err := validateServeTLSFlags(serveTLS, serveTLSCert, serveTLSKey, serveURL); err != nil {
						return err
					}
					if serveAddr == "" {
						// Derive the bind address from the serve URL host:port rather
						// than silently binding 0.0.0.0.
						serveAddr, err = deriveServeBindAddr(serveURL)
						if err != nil {
							return err
						}
					}

					// --serve-tls is the switch; the certificate pair is what the
					// server needs once it is on. Handing isoserve a cert and key
					// without the switch is what used to serve HTTPS behind an
					// http:// advertisement.
					serveCfg := isoserve.Config{BaseURL: serveURL, BindAddr: serveAddr}
					if serveTLS {
						serveCfg.CertFile = serveTLSCert
						serveCfg.KeyFile = serveTLSKey
					}
					serve = isoserve.New(serveCfg)
					// The BMC fetches from this server, so the advertised
					// TransferProtocolType follows what the server actually speaks.
					useHTTPS = serve.UsesTLS()

					if err := serve.Start(ctx); err != nil {
						return fmt.Errorf("starting ISO-serve: %w", err)
					}
					defer func() {
						shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						_ = serve.Shutdown(shutdownCtx)
					}()

					url, token, err := serve.Register(absISO, timeout+5*time.Minute)
					if err != nil {
						return fmt.Errorf("registering ISO for serving: %w", err)
					}
					imageURL = url
					serveToken = token
					defer serve.Revoke(serveToken)
				default:
					return fmt.Errorf("provide --image-url with a URL the BMC can reach, or a local ISO path with --redfish-serve-url")
				}

				deployer := redfish.NewDeployer(redfish.Config{
					Endpoint:  endpoint,
					Username:  username,
					Password:  password,
					Vendor:    redfish.VendorType(vendor),
					VerifySSL: verifySSL,
					Timeout:   timeout,
					SystemID:  systemID,
					AuthMode:  redfish.AuthMode(authMode),
				})

				if err := deployer.Connect(ctx); err != nil {
					return fmt.Errorf("connecting to RedFish endpoint: %w", err)
				}
				// Always tear the session down (DELETE) on both success and error.
				defer func() { _ = deployer.Close() }()

				// Inspect the system and gate on the hardware requirements.
				inspector := hardware.NewInspector(deployer)
				sysInfo, err := inspector.InspectSystem(ctx)
				if err != nil {
					return fmt.Errorf("inspecting system: %w", err)
				}

				fmt.Printf("System: %s %s (SN: %s)\n",
					sysInfo.Manufacturer, sysInfo.Model, sysInfo.SerialNumber)

				reqs := &hardware.Requirements{
					MinMemoryGiB:     minMemory,
					MinCPUs:          minCPUs,
					RequiredFeatures: requiredFeatures,
				}
				if err := inspector.ValidateRequirements(sysInfo, reqs); err != nil {
					return fmt.Errorf("validating requirements: %w", err)
				}

				// Deploy: InsertMedia (URL-pull) -> one-time boot -> reset -> Task poll.
				result, err := deployer.Deploy(ctx, redfish.DeployRequest{
					ImageURL:              imageURL,
					BootTarget:            redfish.BootTargetCd,
					BootMode:              bootMode,
					TransferProtocolHTTPS: useHTTPS,
				})
				if err != nil {
					return fmt.Errorf("deploying ISO: %w", err)
				}

				if result.TaskState != "" {
					fmt.Printf("Deployment task finished: %s\n", result.TaskState)
				}
				for _, m := range result.Messages {
					fmt.Printf("  BMC: %s\n", m)
				}
				fmt.Println("Deployment completed successfully")

				return nil
			},
		},
	},
}
