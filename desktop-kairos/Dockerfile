FROM busybox AS downloader

RUN wget https://download-installer.cdn.mozilla.net/pub/firefox/releases/127.0.2/linux-x86_64/en-US/firefox-127.0.2.tar.bz2
RUN bzip2 -d firefox-127.0.2.tar.bz2
RUN tar xvf firefox-127.0.2.tar
RUN mv firefox /

RUN wget https://commondatastorage.googleapis.com/chromium-browser-snapshots/Linux_x64/1319705/chrome-linux.zip
RUN unzip chrome-linux.zip
RUN mv chrome-linux /

FROM quay.io/kairos/ubuntu:24.04-standard-amd64-generic-v3.1.0-alpha1-k3sv1.29.4-k3s1

#xfce4 xinit xorg xauth xserver-xorg xserver-xorg-core dbus-x11 \
RUN apt update && apt install -y \
  selinux-utils selinux-basics selinux-policy-default \
  cage llvm

#COPY --from=downloader /firefox /etc/firefox
#RUN ln -s /etc/firefox/firefox-bin /usr/bin/firefox
COPY --from=downloader /chrome-linux /etc/chrome-linux
RUN ln -s /etc/chrome-linux/chrome /usr/bin/chrome

COPY kairos-agent /usr/bin/
COPY immucore /usr/bin/

# Regenerate initrd to get the custom immucore in
RUN kernel="$(ls /lib/modules | head -n1)" && dracut -f "/boot/initrd-${kernel}" "${kernel}" && ln -sf "initrd-${kernel}" /boot/initrd;

#COPY wails-demo /usr/bin/wails-demo
