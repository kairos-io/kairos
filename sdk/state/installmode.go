package state

import "strings"

const (
	// InstallModeSentinel and InstallModeInteractiveSentinel are the files
	// under /run/cos/ that carry a boot's install intent. immucore writes
	// them, and the bundled 52_installer.yaml cloud-config gates the two
	// installer services on them.
	//
	// They exist so that the intent is decided once, in immucore, instead of
	// each consumer re-deriving it. The intent reaches a boot differently on
	// each medium (see InstallIntentSentinels), and a new medium that neither
	// rule covers silently starts no installer at all, which is what happened
	// to pixiecore netboot in kairos-io/kairos#5373.
	InstallModeSentinel            = "install_mode"
	InstallModeInteractiveSentinel = "install_mode_interactive"
)

// InstallIntentSentinels returns the sentinel file names that carry this
// boot's install intent, for immucore to write under /run/cos/ next to the
// BootState sentinel. An empty result means no installer should start.
//
// ukiInstallMedium says the boot is a Trusted Boot UKI that did not come from
// an installed EFI entry. That is the only install signal such a medium has:
// AuroraBoot builds every UKI entry from the same signed base cmdline, and the
// installer copies norole.efi byte for byte into each installed role, so a
// keyword there is present on every UKI boot and separates nothing. A UKI
// install medium gets the interactive installer, which is the dispatcher
// rather than just a terminal UI: it runs AutoInstall first, so a config
// carrying install.auto: true still installs unattended.
//
// On the GRUB live ISO the cmdline is per menu entry and unsigned, so the
// entry does state the intent, and the keyword is read here, once. It stays
// the input on that medium because a menu entry has no other way to express
// per-entry intent. Tokens are matched whole: install-mode is a prefix of
// install-mode-interactive, and a substring match would start both installers
// on an interactive boot.
//
// An entry naming both keywords yields both sentinels, as it did when the
// cloud-config grepped for both: the interactive stage runs later in the file
// and takes tty1 back from the plain one.
//
// liveCD gates the GRUB branch because an installed Trusted Boot system keeps
// install-mode on its cmdline for the life of the machine
// (kairos-io/kairos#5000); without it, such a system would reinstall on every
// boot.
func InstallIntentSentinels(cmdline string, ukiInstallMedium, liveCD bool) []string {
	if ukiInstallMedium {
		return []string{InstallModeInteractiveSentinel}
	}

	if !liveCD {
		return nil
	}

	var plain, interactive bool
	for _, token := range strings.Fields(cmdline) {
		switch token {
		case "install-mode":
			plain = true
		case "install-mode-interactive", "interactive-install":
			interactive = true
		}
	}

	var sentinels []string
	if plain {
		sentinels = append(sentinels, InstallModeSentinel)
	}
	if interactive {
		sentinels = append(sentinels, InstallModeInteractiveSentinel)
	}
	return sentinels
}
