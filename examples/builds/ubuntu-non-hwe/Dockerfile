FROM ubuntu:26.04@sha256:0e73b71479e7bc5f006bca872e25c3cd680dc052a0116f90d63ae363537db0dd AS base-kairos
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.4.0,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --version "${VERSION}"
# Remove default kernel that Kairos-init installs
RUN apt-get remove -y linux-base linux-image-generic-hwe-22.04 && apt-get autoremove -y
# Install generic linux image instead
RUN apt-get install -y --no-install-recommends linux-image-generic
RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.4.0,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --version "${VERSION}"
