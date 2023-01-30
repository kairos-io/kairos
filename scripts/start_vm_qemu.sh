#!/bin/bash

if [ ! -e disk.img ]; then
    qemu-img create -f qcow2 disk.img 40g
fi

[[ -n "${ENABLE_KVM}" ]] && KVM=(-enable-kvm)

[[ -n ${ENABLE_SPICE} ]] && SPICE=(-vga qxl -spice port=5900,disable-ticketing)

qemu-system-x86_64 \
    -m ${MEMORY:=2096} \
    -smp cores=2 \
    -nographic \
    "${KVM[@]}" \
    -serial mon:stdio \
    -rtc base=utc,clock=rt \
    -chardev socket,path=qga.sock,server,nowait,id=qga0 \
    "${SPICE[@]}" \
    -device virtio-serial \
    -device virtserialport,chardev=qga0,name=org.qemu.guest_agent.0 \
    -drive if=virtio,media=disk,file=disk.img \
    -drive if=ide,media=cdrom,file=${1:-kairos.iso}
