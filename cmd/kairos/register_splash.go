package main

import (
	"github.com/kairos-io/kairos/v4/internal/splash"
)

func init() {
	// splash is a top-level sub-tool rather than a flag on the agent,
	// because it runs in the initramfs where the agent does not, and it is
	// started by a systemd unit rather than by anything the agent drives.
	//
	// Nothing new has to be shipped for this to reach a boot: kairos-init
	// already makes /usr/bin/immucore a symlink to /usr/bin/kairos, and the
	// 28immucore dracut module installs it with inst_check_multiple, so the
	// whole multi-call binary is inside every initramfs and every rootfs
	// already.
	//
	// The "kairos-splash" alias is what the units actually exec. kairos-init
	// installs /usr/bin/kairos-splash as a symlink to this binary, and both
	// splash units and the dracut module name only that path. A downstream
	// rebuilding the OCI image replaces the file there to replace the
	// animation entirely, without editing a unit or forking the module; the
	// alias is what makes the default symlink dispatch here on argv[0].
	register("splash", splash.Main, "kairos-splash")
}
