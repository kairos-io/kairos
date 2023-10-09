#!/bin/bash
# This module selects the proper network module to be used by dracut
# while avoiding using systemd-networkd

# called by dracut
check() {
    return 255
}

# called by dracut
depends() {
    is_qemu_virtualized && echo -n "qemu-net "

    for module in network network-legacy; do
        if dracut_module_included "$module"; then
            network_handler="$module"
            break
        fi
    done

    if [ -z "$network_handler" ]; then
        if check_module "network-legacy"; then
            network_handler="network-legacy"
        else
            network_handler="network"
        fi
    fi
    echo "kernel-network-modules $network_handler"
    return 0
}

# called by dracut
installkernel() {
    return 0
}

# called by dracut
install() {
    dracut_need_initqueue
}
