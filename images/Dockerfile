ARG BASE_IMAGE=ubuntu:20.04
ARG KAIROS_INIT=main

FROM quay.io/kairos/kairos-init:${KAIROS_INIT} AS kairos-init

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION
ARG TARGETARCH

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
   /kairos-init -l debug -m "${MODEL}" -t "${TRUSTED_BOOT}" --version "${VERSION}" && /kairos-init validate -t "${TRUSTED_BOOT}"
#   /kairos-init -l debug -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}" && /kairos-init validate -t "${TRUSTED_BOOT}"

# COPY kairos-init /kairos-init
# RUN /kairos-init -l debug -m "${MODEL}" -t "${TRUSTED_BOOT}" --version "${VERSION}" && /kairos-init validate -t "${TRUSTED_BOOT}"

# Install git, build tools, and Go 1.25.1
# Handle different package managers for Ubuntu, Fedora, and Alpine
RUN if [ -f /etc/debian_version ]; then \
        apt-get update && apt-get install -y git build-essential wget; \
    elif [ -f /etc/redhat-release ]; then \
        dnf update -y && dnf install -y git gcc gcc-c++ make wget; \
    elif [ -f /etc/alpine-release ]; then \
        apk update && apk add --no-cache git build-base wget; \
    fi && \
    ARCH=${TARGETARCH:-$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')} && \
    wget https://go.dev/dl/go1.25.1.linux-${ARCH}.tar.gz && \
    tar -C /usr/local -xzf go1.25.1.linux-${ARCH}.tar.gz && \
    rm go1.25.1.linux-${ARCH}.tar.gz && \
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile

# Clone kairos-agent repository from cryptsetup-v2.8 branch
RUN git clone -b cryptsetup-v2.8 https://github.com/kairos-io/kairos-agent.git /tmp/kairos-agent

# Build kairos-agent
RUN cd /tmp/kairos-agent && /usr/local/go/bin/go mod download && /usr/local/go/bin/go build -o kairos-agent .

# Move the built binary to /usr/bin/kairos-agent
RUN mv /tmp/kairos-agent/kairos-agent /usr/bin/kairos-agent && chmod +x /usr/bin/kairos-agent

# Clean up
RUN rm -rf /tmp/kairos-agent
