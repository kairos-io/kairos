ARG BASE_IMAGE=ubuntu:20.04

FROM quay.io/kairos/kairos-init:main AS kairos-init

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO=k3s
ARG KUBERNETES_VERSION=latest
ARG VERSION

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN /kairos-init -l debug -s init -m "${MODEL}"  -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN /kairos-init -l debug --validate -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN rm /kairos-init
