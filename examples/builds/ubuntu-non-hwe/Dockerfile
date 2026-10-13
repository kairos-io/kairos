FROM ubuntu:22.04@sha256:5ec03bb3441e8b0bf3b4f9cd4629a1ae763010dc3035bb8da3ae6cf026486401 AS base-kairos
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.3.0,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --version "${VERSION}"
# Remove default kernel that Kairos-init installs
RUN apt-get remove -y linux-base linux-image-generic-hwe-22.04 && apt-get autoremove -y
# Install generic linux image instead
RUN apt-get install -y --no-install-recommends linux-image-generic
RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.3.0,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"
