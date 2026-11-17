package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	"github.com/kairos-io/AuroraBoot/pkg/redfish"
	"github.com/urfave/cli/v2"
)

// redfishProbeCmd builds the read-only `auroraboot redfish probe` subcommand. It
// connects to a BMC, prints what it actually exposes, and emits a starter quirk
// profile (tier C) the operator can tweak. It performs NO writes — no InsertMedia,
// no boot PATCH, no Reset — so it is safe to run against any BMC. Credential and TLS
// handling mirror `redfish deploy` exactly.
func redfishProbeCmd() *cli.Command {
	return &cli.Command{
		Name:  "probe",
		Usage: "Read-only diagnostic: report what a RedFish BMC exposes and emit a starter quirk profile",
		Description: "Connects to a BMC and prints its SessionService/auth mode, ComputerSystem Ids, " +
			"hardware summary, virtual-media layout, and allowable ResetTypes, then emits a starter " +
			"quirk-profile YAML (tier C: UNVERIFIED). It makes no writes (no InsertMedia, boot change, " +
			"or reset) and turns an unsupported BMC into self-service.",
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
				Name:  "system-id",
				Usage: "Redfish ComputerSystem Id to describe; when the BMC exposes more than one system and this is unset, the probe reports all Ids and describes the first",
			},
			&cli.StringFlag{
				Name:  "auth-mode",
				Usage: "Redfish authentication mode: session, basic, or auto (detect from the ServiceRoot and fall back to basic when no SessionService is advertised)",
				Value: "auto",
			},
			&cli.BoolFlag{
				Name:  "verify-ssl",
				Usage: "Verify SSL certificates",
				Value: true,
			},
			&cli.StringFlag{
				Name:  "output",
				Usage: "Output format: text (human report only), yaml (starter profile only, pipeable to a file), or both",
				Value: "both",
			},
			&cli.StringFlag{
				Name:    "quirks-dir",
				Usage:   "Directory of operator-supplied *.yaml/*.yml quirk profiles to load and validate at start. The probe is read-only and does not apply a profile; loading here surfaces malformed-profile errors and the support tier of each",
				EnvVars: []string{redfishQuirksDirEnv},
			},
		},
		Action: func(c *cli.Context) error {
			return runRedfishProbe(c, os.Stdin, c.App.Writer)
		},
	}
}

// runRedfishProbe executes the probe. stdin is injected for password-stdin testing;
// out is the writer the report/YAML are emitted to (c.App.Writer in production).
func runRedfishProbe(c *cli.Context, stdin io.Reader, out io.Writer) error {
	ctx := c.Context

	output, err := resolveProbeOutput(c.String("output"))
	if err != nil {
		return err
	}

	// Load (and so validate) any operator quirk profiles at start. The probe is
	// read-only and applies no profile; loading surfaces malformed-profile errors
	// and each profile's support tier via the registry's load-time log lines.
	if err := loadRedfishQuirksDir(c.String("quirks-dir")); err != nil {
		return err
	}

	endpoint := c.String("endpoint")
	username := c.String("username")
	password, err := resolveRedfishPassword(
		c.String("password"),
		c.String("password-file"),
		c.Bool("password-stdin"),
		stdin,
	)
	if err != nil {
		return err
	}

	// SSRF-guard the operator-supplied BMC endpoint, mirroring `redfish deploy`.
	if err := isoserve.ValidateMediaURL(endpoint); err != nil {
		return fmt.Errorf("validating endpoint: %w", err)
	}

	deployer := redfish.NewDeployer(redfish.Config{
		Endpoint:  endpoint,
		Username:  username,
		Password:  password,
		VerifySSL: c.Bool("verify-ssl"),
		SystemID:  c.String("system-id"),
		AuthMode:  redfish.AuthMode(c.String("auth-mode")),
	})

	if err := deployer.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to RedFish endpoint: %w", err)
	}
	// Always tear the session down (DELETE) on both success and error.
	defer func() { _ = deployer.Close() }()

	report, err := deployer.Probe(ctx)
	if err != nil {
		return fmt.Errorf("probing RedFish endpoint: %w", err)
	}

	renderProbeReport(out, report, output)
	return nil
}

// resolveProbeOutput validates and normalises the --output flag. The empty string
// defaults to "both". Any other value than text/yaml/both is rejected.
func resolveProbeOutput(flag string) (string, error) {
	switch out := strings.ToLower(strings.TrimSpace(flag)); out {
	case "":
		return "both", nil
	case "text", "yaml", "both":
		return out, nil
	default:
		return "", fmt.Errorf("invalid --output %q (valid: text, yaml, both)", flag)
	}
}

// renderProbeReport writes the probe report to out in the requested format
// ("text", "yaml", or "both"). It is the rendering seam, separated from the
// connect/probe machinery so it can be unit-tested without a live BMC.
func renderProbeReport(out io.Writer, report *redfish.ProbeReport, output string) {
	var b strings.Builder
	if output == "text" || output == "both" {
		writeProbeText(&b, report)
	}
	if output == "both" {
		b.WriteString("\n")
	}
	if output == "yaml" || output == "both" {
		if output == "both" {
			b.WriteString(redfish.StarterProfileHeader + "\n")
		}
		b.WriteString(report.StarterProfile())
	}
	// strings.Builder never errors; the single sink write is guarded for the rare
	// io.Writer that does (a closed pipe), matching the cmd house style.
	_, _ = io.WriteString(out, b.String())
}

// writeProbeText renders the human-readable, labelled probe report into b.
func writeProbeText(b *strings.Builder, r *redfish.ProbeReport) {
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(b, format, args...) }

	p("RedFish probe: %s\n", r.Endpoint)

	p("\nService:\n")
	p("  SessionService advertised: %s\n", yesNo(r.HasSessionService))
	if r.HasSessionService {
		p("    auth modes available: session, basic\n")
	} else {
		p("    auth modes available: basic only (no SessionService advertised)\n")
	}
	p("  auth mode used: %s\n", r.AuthModeUsed)

	p("\nSystems:\n")
	p("  member Ids: %s\n", strings.Join(r.SystemIDs, ", "))
	if r.MultipleSystems {
		p("  multiple systems — set --system-id (BMCTarget.SystemID); showing %s\n", r.SelectedSystemID)
	} else {
		p("  describing: %s\n", r.SelectedSystemID)
	}

	p("\nInspect:\n")
	p("  manufacturer: %s\n", orDash(r.System.Manufacturer))
	p("  model:        %s\n", orDash(r.System.Model))
	p("  serial:       %s\n", orDash(r.System.SerialNumber))
	p("  memory (GiB): %d\n", r.System.MemoryGiB)
	p("  CPUs:         %d\n", r.System.ProcessorCount)
	p("  features:     %s\n", orDash(strings.Join(sortedFeatures(r.System.Features), ", ")))
	p("  firmware:     %s\n", orDash(r.FirmwareVersion))

	p("\nVirtual media:\n")
	if len(r.Media) == 0 {
		p("  (none exposed)\n")
	}
	for i, m := range r.Media {
		marker := "  "
		if i == r.DefaultCDIndex {
			marker = "* " // the default search would pick this member
		}
		types := orDash(strings.Join(m.MediaTypes, ","))
		p("%sID=%s location=%s mediaTypes=[%s] inserted=%s\n",
			marker, m.ID, m.Location, types, yesNo(m.Inserted))
	}
	if r.DefaultCDIndex >= 0 {
		p("  (* = the default CD/DVD search would pick this member)\n")
	} else {
		p("  no CD/DVD-capable media found — a deploy would fail to find media\n")
	}
	if r.ManagerHostedCDOnly {
		p("  NOTE: the only CD/DVD media is Manager-hosted (the HPE iLO signal);\n")
		p("        the starter profile suggests a manager-first mediaSearch order.\n")
	}

	p("\nReset:\n")
	p("  power state:           %s\n", orDash(r.PowerState))
	p("  allowable ResetTypes:  %s\n", orDash(strings.Join(r.AllowableResetTypes, ", ")))
	p("  core default for state: %s\n", orDash(r.DefaultResetType))
}

// sortedFeatures returns the detected feature names sorted for stable output.
func sortedFeatures(features map[string]bool) []string {
	out := make([]string, 0, len(features))
	for f, ok := range features {
		if ok {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
