ARG BASE_IMAGE=ubuntu:20.04
ARG KAIROS_INIT=v0.6.8

FROM quay.io/kairos/kairos-init:${KAIROS_INIT} AS kairos-init

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    if [ -n "${KUBERNETES_DISTRO}" ]; then \
        K8S_FLAG="-p ${KUBERNETES_DISTRO}"; \
        if [ "${KUBERNETES_DISTRO}" = "k0s" ] && [ -n "${KUBERNETES_VERSION}" ]; then \
            K8S_VERSION_FLAG="--provider-k0s-version \"${KUBERNETES_VERSION}\""; \
        elif [ "${KUBERNETES_DISTRO}" = "k3s" ] && [ -n "${KUBERNETES_VERSION}" ]; then \
            K8S_VERSION_FLAG="--provider-k3s-version \"${KUBERNETES_VERSION}\""; \
        else \
            K8S_VERSION_FLAG=""; \
        fi; \
    else \
        K8S_FLAG=""; \
        K8S_VERSION_FLAG=""; \
    fi; \
    eval /kairos-init -l debug -s install -m \"${MODEL}\" -t \"${TRUSTED_BOOT}\" ${K8S_FLAG} ${K8S_VERSION_FLAG} --version \"${VERSION}\" && \
    eval /kairos-init -l debug -s init -m \"${MODEL}\" -t \"${TRUSTED_BOOT}\" ${K8S_FLAG} ${K8S_VERSION_FLAG} --version \"${VERSION}\"
