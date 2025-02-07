ARG BASE_IMAGE=ubuntu:20.04

FROM quay.io/kairos/kairos-init:v0.2.2 AS kairos-init

FROM ${BASE_IMAGE} AS base
ARG VARIANT=core
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_PROVIDER=k3s
ARG KUBERNETES_VERSION=latest

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_PROVIDER}" --k8sversion "${KUBERNETES_VERSION}"
RUN /kairos-init -l debug -s init -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_PROVIDER}" --k8sversion "${KUBERNETES_VERSION}"
RUN /kairos-init -l debug --validate -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_PROVIDER}" --k8sversion "${KUBERNETES_VERSION}"
RUN rm /kairos-init