package netboot

import (
	"regexp"
	"strings"
)

// pixiecoreTemplate matches the {{ ID "..." }} placeholders pixiecore expands
// into the URLs it serves. They contain spaces, so they have to be removed
// before a cmdline can be split into options.
var pixiecoreTemplate = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// grubLinuxCmd matches the grub command that loads the kernel inside a
// menuentry, in every spelling grub accepts.
var grubLinuxCmd = regexp.MustCompile(`^linux(efi|16)?\s`)

// mediumOnlyOptions are options of the livecd grub config that must not reach a
// netbooted node. They either describe the physical medium the rootfs sits on,
// which a netbooted node does not have, or they select a boot mode that netboot
// picks for itself.
var mediumOnlyOptions = map[string]bool{
	"cdroot":                       true,
	"root":                         true,
	"rd.live.dir":                  true,
	"rd.live.squashimg":            true,
	"rd.live.image":                true,
	"iso-scan/filename":            true,
	"install-mode":                 true,
	"install-mode-interactive":     true,
	"kairos.boot_live_mode":        true,
	"kairos.remote_recovery_mode":  true,
	"kairos.ram":                   true,
	"kairos.ram.create_partitions": true,
}

// multiValueOptions may legitimately appear more than once, so a livecd value
// for one is kept even when the netboot cmdline already sets the same key. It
// still has to be a value the netboot cmdline does not already carry: the
// kernel makes the last console= it sees the primary one, so appending a
// duplicate of one netboot already sets would move the primary console for no
// reason. A console= the livecd config alone has comes from --live-console,
// where becoming the primary one is the point.
var multiValueOptions = map[string]bool{
	"console": true,
}

// LiveCmdline returns base with the kernel options of the livecd grub config
// appended, so a netbooted node boots with the same options as the same ISO
// booted off a physical medium. See kairos-io/kairos#2573.
//
// base wins: it holds everything netboot must control, so a livecd option
// setting a key base already sets is dropped. Options that only make sense on
// the medium itself go too, as do grub variables and build-time placeholders
// that were never expanded. A grub config with no kernel line in it returns
// base unchanged.
func LiveCmdline(base, grubCfg string) string {
	extra := liveOptions(grubCfg, base)
	if len(extra) == 0 {
		return base
	}

	return base + " " + strings.Join(extra, " ")
}

// liveOptions filters the default menuentry's options down to the ones worth
// carrying over to a netboot, keeping the order they appear in.
func liveOptions(grubCfg, base string) []string {
	var kept []string
	takenKeys, takenOptions := optionSets(base)

	for _, option := range defaultEntryOptions(grubCfg) {
		// $thor_options and the {{NOMODESET}} style placeholders of
		// grub_live_bios.cfg are only resolved when grub reads the config or
		// when the ISO is built. An unresolved one would land on the kernel
		// cmdline as literal text.
		if strings.ContainsAny(option, "${}") {
			continue
		}

		key := optionKey(option)
		if mediumOnlyOptions[key] {
			continue
		}
		if takenOptions[option] {
			continue
		}
		if !multiValueOptions[key] && takenKeys[key] {
			continue
		}

		takenKeys[key] = true
		takenOptions[option] = true
		kept = append(kept, option)
	}

	return kept
}

// defaultEntryOptions returns the kernel options of the grub config's default
// menuentry. grub_live_bios.cfg sets default=0, so that is the first entry that
// loads a kernel, and therefore the first kernel line in the file.
func defaultEntryOptions(grubCfg string) []string {
	for _, line := range strings.Split(grubCfg, "\n") {
		line = strings.TrimSpace(line)
		if !grubLinuxCmd.MatchString(line) {
			continue
		}

		// Drop the command itself and the kernel path that follows it.
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil
		}

		return fields[2:]
	}

	return nil
}

// optionSets returns the keys and the full options a cmdline already sets.
func optionSets(cmdline string) (keys, options map[string]bool) {
	keys, options = map[string]bool{}, map[string]bool{}
	for _, option := range strings.Fields(pixiecoreTemplate.ReplaceAllString(cmdline, "")) {
		keys[optionKey(option)] = true
		options[option] = true
	}

	return keys, options
}

// optionKey returns the name of a cmdline option, with any value stripped.
func optionKey(option string) string {
	if name, _, found := strings.Cut(option, "="); found {
		return name
	}

	return option
}
