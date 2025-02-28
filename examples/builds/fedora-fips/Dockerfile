FROM quay.io/kairos/kairos-init:v0.2.6 AS kairos-init

FROM fedora:40
ARG VERSION=v0.0.1

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install --fips --version "${VERSION}"

# Copy the custom dracut config file which enables fips
COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf

RUN /kairos-init -l debug -s init --version "${VERSION}"
RUN rm /kairos-init

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac