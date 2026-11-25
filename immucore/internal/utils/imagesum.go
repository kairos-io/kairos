package utils

import (
	"fmt"
	"os"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	"github.com/twpayne/go-vfs/v5"
)

// The cmdline keys are image-checksum.verify= and image-checksum.require=, the
// dot notation of the cloud config keys (see imagesum.ParseCmdline). There is
// one spelling per key.

// Where a VerifySettings value came from, for logs and the failure screen.
const (
	SourceCmdline = "kernel cmdline"
	SourcePolicy  = "image-checksum.conf on the state partition"
	SourceDefault = "default"
)

// VerifySettings is what immucore does about the checksum of the image it is
// about to mount.
type VerifySettings struct {
	Action  imagesum.Action
	Require bool
	// ActionSource and RequireSource say where each value was read from.
	ActionSource  string
	RequireSource string
}

// DefaultVerifyAction is used when neither the cmdline nor the policy file set one.
const DefaultVerifyAction = imagesum.None

// ResolveVerifySettings works out the settings for the images in imageDir.
// Precedence, per setting: kernel cmdline, then the policy file kairos-agent keeps
// next to the images (written from the image-checksum cloud config at install,
// upgrade and reset), then the default (none, not required). The cloud config
// itself is not an option: it is on the OEM partition, which is mounted after the
// root image this check has to run before.
//
// A cmdline action that is not none, warn or fail is logged and ignored, so the
// next source applies. A policy file that cannot be read is logged and ignored.
func ResolveVerifySettings(imageDir string) VerifySettings {
	s := VerifySettings{Action: DefaultVerifyAction, ActionSource: SourceDefault, RequireSource: SourceDefault}

	if policy, found, err := imagesum.ReadPolicy(vfs.OSFS, imageDir); err != nil {
		KLog.Logger.Warn().Err(err).Str("dir", imageDir).Msg("Ignoring the image checksum policy file")
	} else if found {
		s.Action, s.Require = policy.Verify, policy.Require
		s.ActionSource, s.RequireSource = SourcePolicy, SourcePolicy
	}

	cmdline, _ := os.ReadFile(GetHostProcCmdline())
	o := imagesum.ParseCmdline(string(cmdline))
	for _, problem := range o.Problems {
		KLog.Logger.Warn().Str("arg", problem).Msg("Ignoring an image checksum setting on the cmdline")
	}
	if o.Verify != nil {
		s.Action, s.ActionSource = *o.Verify, SourceCmdline
	}
	if o.Require != nil {
		s.Require, s.RequireSource = *o.Require, SourceCmdline
	}
	return s
}

// ImageChecksumProblem says what is wrong with an image, for the failure screen.
type ImageChecksumProblem struct {
	Image string
	// Kind is a short title, e.g. "checksum mismatch".
	Kind string
	// Detail explains the problem in a sentence or two.
	Detail string
	// Expected is the stored sha256, Actual the one computed from the image now.
	// Either can be empty when it does not apply.
	Expected string
	Actual   string
}

// RenderImageChecksumFailedMessage builds the boot failure screen shown when the
// action is fail and the image has a problem.
func RenderImageChecksumFailedMessage(p ImageChecksumProblem, s VerifySettings) string {
	sidecar := imagesum.SidecarPath(p.Image)

	var wrong strings.Builder
	wrong.WriteString("Immucore checks the OS image against the sha256 recorded for it\n")
	wrong.WriteString("before it mounts it, and the check did not pass.\n\n")
	fmt.Fprintf(&wrong, "  Problem:\n    %s\n", p.Kind)
	fmt.Fprintf(&wrong, "  Image:\n    %s\n", p.Image)
	fmt.Fprintf(&wrong, "  Expected sha256 (stored in %s):\n    %s\n", baseName(sidecar), orNone(p.Expected))
	fmt.Fprintf(&wrong, "  Actual sha256 (computed from the image just now):\n    %s\n", orNone(p.Actual))
	if p.Detail != "" {
		fmt.Fprintf(&wrong, "\n%s\n", p.Detail)
	}
	fmt.Fprintf(&wrong, "\n  Action: %s (from %s)\n", s.Action, s.ActionSource)
	if s.Require {
		fmt.Fprintf(&wrong, "  A missing or stale checksum is a problem too: require is on\n  (from %s)\n", s.RequireSource)
	}

	var fix strings.Builder
	fix.WriteString("  - Reboot and pick the passive or recovery entry in the boot menu.\n")
	fix.WriteString("    Then repair the image with kairos-agent upgrade or reset.\n")
	fmt.Fprintf(&fix, "  - To boot this image anyway, edit the boot entry (press e) and add\n    %s=%s to the kernel line.\n", imagesum.CmdlineVerify, imagesum.Warn)
	fix.WriteString("  - A disk that corrupts images can corrupt more: check its health.\n")

	var logs strings.Builder
	logs.WriteString("  journalctl -b -u immucore\n")
	logs.WriteString("  /run/immucore/\n")

	return RenderFailureScreen(
		"OS image checksum check failed",
		"",
		FailureSection{Title: SectionWhatWentWrong, Body: wrong.String()},
		FailureSection{Title: SectionHowToFix, Body: fix.String()},
		FailureSection{Title: "Where to look", Body: logs.String()},
	)
}

func baseName(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func orNone(s string) string {
	if s == "" {
		return "(not available)"
	}
	return s
}
