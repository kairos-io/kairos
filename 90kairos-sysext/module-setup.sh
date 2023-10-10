#!/bin/bash
# This module tries to add systemd-sysext module to the initramfs if its in the system, otherwise it does nothing

# called by dracut
check() {
    return 0
}

# called by dracut
depends() {
    # If the binary(s) requirements are not fulfilled the module can't be installed.
    require_binaries systemd-sysext || return 1
    # Check if the module files exists
    # This is not normal but on ubuntu-22 the binary for sysext exists but the dracut module doesnt, so we
    # need to do further checks
    files=( "${dracutbasedir}"/modules.d/??systemd-sysext )
    [ "${#files[@]}" -ge 2 ] && return 1
    if [ -d "${files[0]}" ]; then
      echo "systemd-sysext"
      return 0
    fi
    return 1
}

# called by dracut
installkernel() {
    return 0
}

# called by dracut
install() {
    return 0
}
