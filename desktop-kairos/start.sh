#!/bin/bash

export SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
export WORK_DIR=$SCRIPT_DIR/state
export TPMDIR=$WORK_DIR/.tpm-emulation

export CDROM="${1:-build/build/kairos.iso}"

if [[ ! "$WORK_DIR" || ! -d "$WORK_DIR" ]]; then
    mkdir -p $WORK_DIR

    # check if tmp dir was created
    if [[ ! "$WORK_DIR" || ! -d "$WORK_DIR" ]]; then
        echo "Could not create temp dir"
        exit 1
    fi

    if [ ! -e $WORK_DIR/disk.img ]; then
        qemu-img create -f qcow2 "$WORK_DIR/disk.img" 60g
    fi

    mkdir -p $TPMDIR
fi


if pid=$(pidof swtpm); then
    echo "swtpm is running, stopping it"
    kill $pid
fi
swtpm socket --tpmstate dir=$TPMDIR --ctrl type=unixio,path=$TPMDIR/swtpm-sock --log level=20 --tpm2 > /dev/null 2>&1 &

# -nic bridge,br=br0,model=virtio-net-pci \
# -device virtio-serial -nic user,hostfwd=tcp::2223-:22 \

# To debug ovmf firmware:
# -bios /usr/share/edk2-ovmf-fedora-bin/edk2/ovmf/OVMF_CODE.fd \
# -chardev file,path=/home/dimitris/itxaka-log.txt,id=edk2-debug -device isa-debugcon,iobase=0x402,chardev=edk2-debug \
#
#-spice port=9000,addr=127.0.0.1,disable-ticketing=yes \
#-monitor unix:/tmp/qemu-monitor.sock,server=on,wait=off \

qemu-system-x86_64 \
    -enable-kvm \
    -cpu "${CPU:=host}" \
    -serial mon:stdio \
    -m ${MEMORY:=10096} \
    -smp ${CORES:=5} \
    -rtc base=utc,clock=rt \
    -chardev socket,id=chrtpm,path=$TPMDIR/swtpm-sock \
    -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0 \
    -chardev socket,path=qga.sock,server=on,wait=off,id=qga0 \
    -device virtio-serial \
    -device virtserialport,chardev=qga0,name=org.qemu.guest_agent.0 \
    -drive id=disk1,if=none,media=disk,file="$WORK_DIR/disk.img" \
    -device virtio-blk-pci,drive=disk1,bootindex=0 \
    -netdev user,id=net0,hostfwd=tcp::2224-:23 \
    -device e1000,netdev=net0 \
    -drive id=cdrom1,if=none,media=cdrom,file="${CDROM}" \
    -device ide-cd,drive=cdrom1,bootindex=1 \
    -boot menu=on
