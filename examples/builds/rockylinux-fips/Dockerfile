FROM quay.io/kairos/kairos-init:v0.5.7 AS kairos-init

FROM rockylinux:9
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --fips --version "${VERSION}"
# Copy the custom dracut config file
COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac
