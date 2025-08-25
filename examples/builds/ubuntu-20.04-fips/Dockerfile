FROM quay.io/kairos/kairos-init:v0.5.15 AS kairos-init

FROM ubuntu:20.04
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --version "${VERSION}"
# Remove default kernel that Kairos-init installs
RUN apt-get remove -y linux-base linux-image-generic-hwe-20.04 && apt-get autoremove -y
## THIS comes from the Ubuntu documentation: https://canonical-ubuntu-pro-client.readthedocs-hosted.com/en/latest/tutorials/create_a_fips_docker_image.html
## I've just added "linux-image-fips" in the package list
## If you want to use secureboot with fips, you need to install the shim-signed package
## from the Ubuntu PRO repos as its signed with adifferent key
RUN --mount=type=secret,id=pro-attach-config \
    apt-get update \
    && apt-get install --no-install-recommends -y ubuntu-advantage-tools ca-certificates \
    && pro attach --attach-config /run/secrets/pro-attach-config \
    && apt-get upgrade -y \
    && apt-get install -y openssl libssl1.1 libssl1.1-hmac libgcrypt20 libgcrypt20-hmac strongswan strongswan-hmac openssh-client openssh-server linux-image-fips shim-signed \
    && pro detach --assume-yes

# Copy the custom dracut config file which enables fipsn
COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac