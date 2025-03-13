ARG BASE_IMAGE=ubuntu:20.04

FROM quay.io/kairos/kairos-init:v0.3.0 AS kairos-init

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
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug -s init -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug --validate -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN rm /kairos-init
