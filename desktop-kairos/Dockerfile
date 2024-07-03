FROM busybox AS downloader

RUN wget https://commondatastorage.googleapis.com/chromium-browser-snapshots/Linux_x64/1319705/chrome-linux.zip
RUN unzip chrome-linux.zip
RUN mv chrome-linux /

FROM quay.io/kairos/ubuntu:24.04-standard-amd64-generic-v3.1.0-rc1-k3sv1.29.4-k3s1

RUN apt update && apt install -y cage libnss3 libasound2t64

COPY --from=downloader /chrome-linux /etc/chrome-linux
RUN ln -s /etc/chrome-linux/chrome /usr/bin/chrome
