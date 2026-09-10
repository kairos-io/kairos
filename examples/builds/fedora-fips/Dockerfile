FROM quay.io/kairos/kairos-init:v0.7.0-beta13 AS kairos-init

FROM fedora:40@sha256:3c86d25fef9d2001712bc3d9b091fc40cf04be4767e48f1aa3b785bf58d300ed
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --fips --version "${VERSION}"

# Copy the custom dracut config file which enables fips
COPY dracut.conf /etc/dracut.conf.d/kairos-fips.conf

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac
