ARG BASE_IMAGE=ubuntu:20.04
ARG VARIANT
ARG TARGETARCH
ARG FRAMEWORK_VERSION=v2.15.14

FROM quay.io/kairos/kairos-init:v0.2.4 AS kairos-init
FROM --platform="linux/${TARGETARCH}" quay.io/kairos/framework:${FRAMEWORK_VERSION} AS framework

FROM ${BASE_IMAGE} AS base-kairos
ARG VARIANT=core
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO=k3s
ARG KUBERNETES_VERSION=latest
ARG FRAMEWORK_VERSION

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug -s install -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}"
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug -s init -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}"
RUN /kairos-init -f "${FRAMEWORK_VERSION}" -l debug --validate -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}"
RUN rm /kairos-init

FROM base-kairos AS kairos-core

FROM base-kairos AS kairos-standard
COPY --from=framework /usr/bin/luet /usr/bin/luet
COPY --from=framework /etc/luet/luet.yaml /etc/luet/luet.yaml
RUN luet install -y utils/edgevpn utils/k9s utils/nerdctl container/kubectl utils/kube-vip

FROM kairos-${VARIANT} AS kairos-final
