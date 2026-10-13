package utils

import (
	"fmt"
	"strings"
)

// encryptOnBootFailureIntro is the shared intro paragraph for every failure screen
// of the boot time encryption step: names the feature the user enabled and
// where in the flow it broke.
func encryptOnBootFailureIntro() string {
	return "This node is configured with " + Emphasize("kcrypt.encrypt_on_boot") + ". Kairos checked the\n" +
		"partitions listed in " + Emphasize("install.encrypted_partitions") + " and tried to encrypt the\n" +
		"ones that are still plaintext before mounting them, but hit a problem.\n" +
		"There is no plaintext fallback: the boot stops here. Details below."
}

// RenderEncryptOnBootLookupFailedMessage explains that a partition listed in
// install.encrypted_partitions could not be found on this machine, neither as
// a LUKS container nor as a plaintext filesystem.
func RenderEncryptOnBootLookupFailedMessage(labels []string, failErr error) string {
	var wrong strings.Builder
	wrong.WriteString("A partition configured for encryption was not found on this machine:\n")
	fmt.Fprintf(&wrong, "  %s\n", failErr)
	fmt.Fprintf(&wrong, "Configured partitions: %s\n", strings.Join(labels, ", "))

	var fix strings.Builder
	fix.WriteString("  - Check that every label in install.encrypted_partitions matches a\n")
	fix.WriteString("    filesystem label that exists on this machine\n")
	fix.WriteString("  - Remove stale labels from the list, or fix the label on disk\n")
	fix.WriteString("  - Inspect the immucore logs on the next boot for the full error\n")

	return RenderFailureScreen(
		"Encrypt on boot: configured partition not found",
		encryptOnBootFailureIntro(),
		FailureSection{Title: SectionWhatWentWrong, Body: wrong.String()},
		FailureSection{Title: SectionHowToFix, Body: fix.String()},
	)
}

// RenderEncryptOnBootProtectedMessage explains that a partition listed in
// install.encrypted_partitions cannot be encrypted while the system boots,
// because the boot itself depends on it: the mounted OEM configuration
// source, the state or recovery partition holding the root images, or the
// EFI partition the firmware reads.
func RenderEncryptOnBootProtectedMessage(label, reason string) string {
	var wrong strings.Builder
	fmt.Fprintf(&wrong, "install.encrypted_partitions lists %s, but encrypting it during the\n", label)
	fmt.Fprintf(&wrong, "boot is not supported: %s.\n", reason)

	var fix strings.Builder
	fmt.Fprintf(&fix, "  - Encrypt %s at install time instead, or\n", label)
	fmt.Fprintf(&fix, "  - Remove %s from install.encrypted_partitions\n", label)

	return RenderFailureScreen(
		"Encrypt on boot: partition cannot be encrypted during the boot",
		encryptOnBootFailureIntro(),
		FailureSection{Title: SectionWhatWentWrong, Body: wrong.String()},
		FailureSection{Title: SectionHowToFix, Body: fix.String()},
	)
}

// RenderEncryptOnBootConfigFailedMessage explains that reading the
// configuration itself failed, so whether this node opted in to boot time
// encryption is unknown. A node that opted in must never continue booting on
// plaintext partitions, so the step fails closed instead of guessing.
func RenderEncryptOnBootConfigFailedMessage(failErr error) string {
	intro := "Kairos could not read this node's configuration while checking whether\n" +
		Emphasize("kcrypt.encrypt_on_boot") + " is enabled. A node that opted in must never\n" +
		"continue booting on plaintext partitions, so the boot stops here.\n" +
		"Details below."

	var wrong strings.Builder
	wrong.WriteString("Scanning the configuration failed:\n")
	fmt.Fprintf(&wrong, "  %s\n", failErr)

	var fix strings.Builder
	fix.WriteString("  - Check that the OEM partition mounts and that every cloud config\n")
	fix.WriteString("    file on it parses as YAML\n")
	fix.WriteString("  - Inspect the immucore logs on the next boot for the full error\n")

	return RenderFailureScreen(
		"Encrypt on boot: configuration could not be read",
		intro,
		FailureSection{Title: SectionWhatWentWrong, Body: wrong.String()},
		FailureSection{Title: SectionHowToFix, Body: fix.String()},
	)
}

// RenderEncryptOnBootFailedMessage explains that encrypting the pending
// partitions failed. The usual causes are a missing TPM device on the node
// and, in challenger mode, an unreachable KMS.
func RenderEncryptOnBootFailedMessage(labels []string, failErr error) string {
	var wrong strings.Builder
	fmt.Fprintf(&wrong, "Encrypting the pending partitions (%s) failed:\n", strings.Join(labels, ", "))
	fmt.Fprintf(&wrong, "  %s\n", failErr)

	var fix strings.Builder
	fix.WriteString("  - Check this machine has a working TPM 2.0 device (on a VM, that a\n")
	fix.WriteString("    vTPM is attached to this instance)\n")
	fix.WriteString("  - If a kcrypt challenger server is configured, check it is reachable\n")
	fix.WriteString("  - Inspect the immucore logs on the next boot for the full error\n")
	fix.WriteString("  - A partition may hold a partial LUKS header now; reformat it before\n")
	fix.WriteString("    retrying if encryption keeps failing on it\n")

	return RenderFailureScreen(
		"Encrypt on boot: partition encryption failed",
		encryptOnBootFailureIntro(),
		FailureSection{Title: SectionWhatWentWrong, Body: wrong.String()},
		FailureSection{Title: SectionHowToFix, Body: fix.String()},
	)
}
