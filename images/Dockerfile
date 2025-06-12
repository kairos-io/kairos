ARG BASE_IMAGE=ubuntu:20.04
ARG KAIROS_INIT=v0.5.1

FROM quay.io/kairos/kairos-init:${KAIROS_INIT} AS kairos-init

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN /kairos-init -l debug -s init -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}"
RUN /kairos-init validate -t "${TRUSTED_BOOT}"
RUN rm /kairos-init
