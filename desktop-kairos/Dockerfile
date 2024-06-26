FROM quay.io/kairos/ubuntu:24.04-standard-amd64-generic-v3.1.0-alpha1-k3sv1.29.4-k3s1

RUN apt update && apt install -y \
  selinux-utils selinux-basics selinux-policy-default \
  xfce4 xinit xorg xauth xserver-xorg xserver-xorg-core dbus-x11 \
  firefox cage llvm

COPY kairos-agent /usr/bin/
COPY immucore /usr/bin/
COPY authorized_keys /root/.ssh/authorized_keys

# Regenerate initrd to get the custom immucore in
RUN kernel="$(ls /lib/modules | head -n1)" && dracut -f "/boot/initrd-${kernel}" "${kernel}" && ln -sf "initrd-${kernel}" /boot/initrd;

#COPY wails-demo /usr/bin/wails-demo
