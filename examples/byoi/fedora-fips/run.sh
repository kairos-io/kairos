qemu-img create -f qcow2 disk.img 40g

qemu-system-x86_64 -m 8096 -smp cores=2 -nographic -cpu host -enable-kvm -serial mon:stdio -rtc base=utc,clock=rt -chardev socket,path=qga.sock,server,nowait,id=qga0 -device virtio-serial -device virtserialport,chardev=qga0,name=org.qemu.guest_agent.0 -drive if=virtio,media=disk,file=disk.img -drive if=ide,media=cdrom,file=build/iso/kairos.iso
