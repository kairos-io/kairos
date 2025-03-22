ARG BASE_IMAGE=ubuntu:20.04

FROM quay.io/kairos/kairos-init:v0.4.2 AS kairos-init

FROM golang:1.24-bookworm AS golang
ARG AGENT_COMMIT=fix_hooks
RUN apt-get update && apt-get install -y rsync gcc bash git
RUN git clone --branch "${AGENT_COMMIT}" https://github.com/kairos-io/kairos-agent.git /kairos-agent
WORKDIR /kairos-agent
ARG LDFLAGS="-s -w -X github.com/kairos-io/kairos-agent/v2/internal/common.VERSION=${AGENT_COMMIT} -X github.com/kairos-io/kairos-agent/v2/internal/common.gitCommit=$AGENT_COMMIT"
ENV CGO_ENABLED=0
RUN go build -o kairos-agent -ldflags "${LDFLAGS}" main.go

FROM ${BASE_IMAGE} AS base-kairos
ARG VARIANT=core
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO=k3s
ARG KUBERNETES_VERSION=latest
ARG FRAMEWORK_VERSION=v2.17.0
ARG VERSION

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug -s install -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
COPY --from=golang /kairos-agent/kairos-agent /usr/bin/kairos-agent
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug -s init -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug --validate -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN rm /kairos-init
