FROM quay.io/kairos/kairos-init:v0.4.9 AS kairos-init

FROM ubuntu:22.04
ARG VERSION=v0.0.1

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install --version "${VERSION}"
# Remove default kernel that Kairos-init installs
RUN apt-get remove -y linux-base linux-image-generic-hwe-22.04 && apt-get autoremove -y
## THIS comes from the Ubuntu documentation: https://canonical-ubuntu-pro-client.readthedocs-hosted.com/en/latest/tutorials/create_a_fips_docker_image.html
## I've just added "linux-image-fips" in the package list
RUN --mount=type=secret,id=pro-attach-config \
    apt-get update \
    && apt-get install --no-install-recommends -y ubuntu-advantage-tools ca-certificates \
    && pro attach --attach-config /run/secrets/pro-attach-config \
    && apt-get upgrade -y \
    && apt-get install -y strongswan strongswan-hmac openssh-client openssh-server linux-image-fips \
    && pro detach --assume-yes

# Copy the custom dracut modules.fips that includes 2 missing modules
COPY modules.fips /tmp/modules.fips
RUN kernel=$(ls /lib/modules | head -n1) && mv /tmp/modules.fips /lib/modules/${kernel}/modules.fips

RUN /kairos-init -l debug -s init --version "${VERSION}"
RUN rm /kairos-init

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac