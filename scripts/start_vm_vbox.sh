#!/bin/sh
# Author: Ettore Di Giacinto <mudler@c3os.io>
# Simple scripts that runs a VM and keeps the process open for a while
# It is used in tests, but can be useful for local testing

set -e

DISK=$1
CLOUD_INIT=$2

HAS_VBOX="$(type "VBoxManage" > /dev/null 2>&1 && echo true || echo false)"
HAS_MKISO="$(type "mkisofs" > /dev/null 2>&1 && echo true || echo false)"

if [ "${HAS_VBOX}" = "false" ] || \
    [ "${HAS_MKISO}" = "false" ]; then
    echo "vbox and mkisofs are required"
    exit 1
fi

if [ -z "$DISK" ]; then
    echo "error: No OVA file specified"
    echo "usage: $0 file.ova <cloud_init>"
    echo "<cloud_init> is optional"
    exit 1
fi

if [ -n "$CLOUD_INIT" ]; then
    mkdir -p build
    # FIXME: SC3044 (warning): In POSIX sh, 'pushd' is undefined.
    # https://www.shellcheck.net/wiki/SC3044
    # shellcheck disable=SC3044
    pushd build
    touch meta-data
    cp -rfv "${CLOUD_INIT}" user-data

    rm -f ci.iso
    mkisofs -output ci.iso -volid cidata -joliet -rock user-data meta-data
    # FIXME: SC3044 (warning): In POSIX sh, 'popd' is undefined.
    # https://www.shellcheck.net/wiki/SC3044
    # shellcheck disable=SC3044
    popd
fi

machine_id="${MACHINE_ID:-test_vm}"

echo "Importing VM"
VBoxManage import "${DISK}" --vsys 0 --vmname "${machine_id}"

if [ -n "$CLOUD_INIT" ]; then
    VBoxManage storageattach "${machine_id}" --storagectl "sata controller" --port 1 --device 0 --type dvddrive --medium build/ci.iso
fi

VBoxManage startvm "${machine_id}" --type headless
sleep 10

set +e

# FIXME: SC3006 (warning): In POSIX sh, standalone ((..)) is undefined.
# https://www.shellcheck.net/wiki/SC3006
# shellcheck disable=SC3006
((count = 100))
# FIXME: SC3010 (warning): In POSIX sh, [[ ]] is undefined
# https://www.shellcheck.net/wiki/SC3010
# shellcheck disable=SC3010
while [[ $count -ne 0 ]]; do
    VBoxManage showvminfo "${machine_id}" | grep -c "running (since"
    rc=$?
    # FIXME: SC3010 (warning): In POSIX sh, [[ ]] is undefined
    # https://www.shellcheck.net/wiki/SC3010
    # shellcheck disable=SC3010
    if [[ $rc -eq 1 ]] ; then
        # FIXME: SC3006 (warning): In POSIX sh, standalone ((..)) is undefined.
        # https://www.shellcheck.net/wiki/SC3006
        # shellcheck disable=SC3006
        ((count = 1))
        echo "Machine stopped"
        break
    fi
    # FIXME: SC3006 (warning): In POSIX sh, standalone ((..)) is undefined.
    # https://www.shellcheck.net/wiki/SC3006
    # shellcheck disable=SC3006
    ((count = count - 1))
    sleep 5
done
