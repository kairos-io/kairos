FROM quay.io/kairos/kairos-init:v0.5.16 AS kairos-init

FROM ubuntu:22.04 AS base-kairos
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --version "${VERSION}"
# Remove default kernel that Kairos-init installs
RUN apt-get remove -y linux-base linux-image-generic-hwe-22.04 && apt-get autoremove -y
# Install generic linux image instead
RUN apt-get install -y --no-install-recommends linux-image-generic
RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"
