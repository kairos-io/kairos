#!/bin/bash
# Author: Ettore Di Giacinto <mudler@c3os.io>
# Simple kairos builder of ova and qcow images from ISOs on VBox
# mainly purpose is to run it on github actions in macOS runners.

set -e

ISO=$1
SSH_USER="${SSH_USER:-kairos}"
SSHPASS="${SSH_PASS:-kairos}"
export SSHPASS
HD_SIZE="${HD_SIZE:-50000}"
INSTALL_COMMAND="${INSTALL_COMMAND:-sudo /bin/sh -c 'elemental install /dev/sda && sync'}"

HAS_SSHPASS="$(type "sshpass" &> /dev/null && echo true || echo false)"
HAS_VBOX="$(type "VBoxManage" &> /dev/null && echo true || echo false)"
HAS_QEMU="$(type "qemu-img" &> /dev/null && echo true || echo false)"

if [ "$HAS_SSHPASS" == "false" ] || \
    [ "$HAS_VBOX" == "false" ] || \
    [ "$HAS_QEMU" == "false" ]; then
    echo "vbox, qemu and sshpass are required"
    exit 1
fi

if [ -z "$ISO" ]; then
    echo "error: No iso file specified"
    echo "usage: $0 file.iso"
    exit 1
fi

outdir=$(dirname $ISO)
outfile=$(basename $ISO .iso)

tmpdir=$(mktemp -d)
machine_id="$outfile"

function cleanup {
     VBoxManage controlvm "${machine_id}" poweroff &>/dev/null || true
	 VBoxManage unregistervm "${machine_id}" --delete &>/dev/null || true
	 VBoxManage closemedium disk $tmpdir/sda.vdi --delete &>/dev/null || true
     rm -rfv $tmpdir || true
}

# Call the cleanup function
trap cleanup EXIT

echo "Creating VM"
VBoxManage createmedium disk --filename $tmpdir/sda.vdi --size ${HD_SIZE}

if [[ "${machine_id}" == *"ubuntu"* ]]; then
    VBoxManage createvm --name "${machine_id}" --register --ostype Ubuntu_64
else
    VBoxManage createvm --name "${machine_id}" --register
fi
VBoxManage modifyvm "${machine_id}" --memory 10240 --cpus 3
VBoxManage modifyvm "${machine_id}" --nic1 nat --boot1 disk --boot2 dvd --natpf1 "guestssh,tcp,,2222,,22"
VBoxManage storagectl "${machine_id}" --name "sata controller" --add sata --portcount 2 --hostiocache off
VBoxManage storageattach "${machine_id}" --storagectl "sata controller" --port 0 --device 0 --type hdd --medium $tmpdir/sda.vdi
VBoxManage storageattach "${machine_id}" --storagectl "sata controller" --port 1 --device 0 --type dvddrive --medium $ISO
VBoxManage startvm "${machine_id}" --type headless

set +e
((count = 100))                        
while [[ $count -ne 0 ]] ; do
    echo "Running ssh command"
    sshpass -e ssh -o StrictHostKeyChecking=no -o GlobalKnownHostsFile=/dev/null -o UserKnownHostsFile=/dev/null ${SSH_USER}@127.0.0.1 -p 2222 $INSTALL_COMMAND
    rc=$?
    echo "Done running command"
    if [[ $rc -eq 0 ]] ; then
        ((count = 1))
        break
    fi
    ((count = count - 1))
    sleep 5
done

if [[ $rc -eq 0 ]] ; then
    echo "Installation succeeded"
else
    echo "Installation failed"
    exit 1
fi

set -e

VBoxManage controlvm "${machine_id}" poweroff &>/dev/null || true
VBoxManage storageattach "${machine_id}" --storagectl 'sata controller' --port 1 --device 0 --type dvddrive --medium emptydrive --forceunmount || true

echo "Exporting $outdir/$outfile.qcow2.tar.xz..."
qemu-img convert -f vdi -O qcow2 $tmpdir/sda.vdi $outdir/$outfile.qcow2
tar -cJf $outdir/$outfile.qcow2.tar.xz $outdir/$outfile.qcow2
rm -rf $outdir/$outfile.qcow2
echo "Exporting $outdir/$outfile.ova..."
VBoxManage export "${machine_id}" -o $outdir/$outfile.ova
