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
	register("splash", splash.Main)
}
