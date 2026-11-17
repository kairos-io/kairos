package constants_test

import (
	"strings"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// biosCommands are the grub commands the BIOS core image can run. It is the
// committed eltorito.img, built once from the fixed grub2-mkimage module list
// documented at the top of constants.go, and an ISO ships no i386-pc module
// directory, so grub cannot autoload anything that is not in that list.
//
// Grouped by the module that provides each command, so that adding a module to
// the eltorito.img recipe has an obvious counterpart here.
var biosCommands = map[string]bool{
	// kernel and normal: the menu language itself
	"menuentry":       true,
	"submenu":         true,
	"hiddenentry":     true,
	"set":             true,
	"export":          true,
	"terminal_output": true,
	// echo
	"echo": true,
	// linux
	"linux":  true,
	"initrd": true,
	// search, search_label, search_fs_file, search_fs_uuid
	"search": true,
	// font
	"loadfont": true,
	// configfile
	"configfile": true,
	// loadenv
	"load_env": true,
	"save_env": true,
	// chain
	"chainloader": true,
	// test, true
	"test": true,
	"true": true,
	// ls
	"ls": true,
}

// grubKeywords are parsed by the menu interpreter rather than dispatched as
// commands, so no module has to provide them.
var grubKeywords = map[string]bool{
	"if": true, "fi": true, "else": true, "elif": true, "then": true,
	"while": true, "done": true, "function": true, "}": true, "{": true,
}

var _ = Describe("Live grub config", Label("constants"), func() {
	// The file is the live config for both firmware paths: prepareBootArtifacts
	// writes it to /boot/grub2/grub.cfg and GrubEfiCfg chainloads that same
	// path. The EFI binary carries more modules than the BIOS core image, so a
	// command only the EFI one has must sit behind a grub_platform test.
	// Without that, BIOS live boots printed "error: can't find command
	// `smbios'." before the menu on every boot (kairos-io/kairos#4758).
	It("only calls commands the BIOS core image has, outside an EFI-only block", func() {
		var efiOnly int
		var offenders []string

		for _, raw := range strings.Split(string(constants.GrubLiveBiosCfg), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			fields := strings.Fields(line)
			cmd := fields[0]

			// Track the depth of `if [ "${grub_platform}" = "efi" ]` blocks.
			// Nested ifs inside one still count as guarded, which is why this
			// is a depth and not a boolean.
			if cmd == "if" && strings.Contains(line, "${grub_platform}") && strings.Contains(line, "efi") {
				efiOnly++
				continue
			}
			if efiOnly > 0 {
				if cmd == "if" {
					efiOnly++
				}
				if cmd == "fi" {
					efiOnly--
				}
				continue
			}

			if grubKeywords[cmd] || biosCommands[cmd] {
				continue
			}
			offenders = append(offenders, cmd)
		}

		Expect(offenders).To(BeEmpty(),
			"grub_live_bios.cfg calls %v unguarded; the BIOS core image cannot load it, "+
				"so a BIOS live boot prints \"error: can't find command\" before the menu. "+
				"Either add the module to the eltorito.img recipe and to biosCommands, "+
				"or wrap the call in `if [ \"${grub_platform}\" = \"efi\" ]`.", offenders)
	})

	It("still looks the model up on EFI, so Thor detection keeps working", func() {
		cfg := string(constants.GrubLiveBiosCfg)
		Expect(cfg).To(ContainSubstring("smbios --type 4 --get-string 5 --set model"))
		Expect(cfg).To(ContainSubstring(`[ "${model}" = "Thor" ]`))
	})
})
