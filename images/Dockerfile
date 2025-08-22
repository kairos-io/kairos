ARG BASE_IMAGE=ubuntu:20.04
ARG KAIROS_INIT=v0.5.14
ARG IMMUCORE_BRANCH=test-kairos-sdk-pr-611
ARG KAIROS_AGENT_BRANCH=test-kairos-sdk-pr-611
ARG KAIROS_INIT_BRANCH=feat/support_multipath

# Stage 1: Build kairos-init from specific branch
FROM golang:1.24 AS kairos-init-builder
ARG KAIROS_INIT_BRANCH
WORKDIR /app/kairos-init
RUN git clone --branch ${KAIROS_INIT_BRANCH} https://github.com/aidanleuck/kairos-init.git .
RUN go build -o kairos-init

# Stage 2: Build immucore from specific branch
FROM golang:1.24 AS immucore-builder
ARG IMMUCORE_BRANCH
WORKDIR /app/immucore
RUN git clone --branch ${IMMUCORE_BRANCH} https://github.com/kairos-io/immucore.git .
RUN go build -o immucore

# Stage 3: Build kairos-agent from specific branch  
FROM golang:1.24 AS kairos-agent-builder
ARG KAIROS_AGENT_BRANCH
WORKDIR /app/kairos-agent
RUN git clone --branch ${KAIROS_AGENT_BRANCH} https://github.com/kairos-io/kairos-agent.git .
RUN go build -o kairos-agent

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION=v1.2.3

# Copy custom built kairos-init
COPY --from=kairos-init-builder /app/kairos-init/kairos-init /kairos-init

RUN /kairos-init -l debug -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}" && /kairos-init validate -t "${TRUSTED_BOOT}"

# Copy custom built binaries
COPY --from=immucore-builder /app/immucore/immucore /usr/bin/immucore
COPY --from=kairos-agent-builder /app/kairos-agent/kairos-agent /usr/bin/kairos-agent


RUN /kairos-init -l debug -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}" && /kairos-init after-init -t "${TRUSTED_BOOT}"
