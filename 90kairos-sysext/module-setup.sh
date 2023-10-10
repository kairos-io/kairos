#!/bin/bash
# This module tries to add systemd-sysext module to the initramfs if its in the system, otherwise it does nothing

# called by dracut
check() {
    # Return 255 to only include the module, if another module requires it.
    return 255
}

# called by dracut
depends() {
    # If the binary(s) requirements are not fulfilled the module can't be installed.
    require_binaries systemd-sysext || return 1
    echo "systemd-sysext"
    return 0
}

# called by dracut
installkernel() {
    return 0
}

# called by dracut
install() {
    return 0
}
