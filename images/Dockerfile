ARG BASE_IMAGE=ubuntu:20.04
# KAIROS_INIT is the legacy input: a tag on quay.io/kairos/kairos-init.
# KAIROS_INIT_IMAGE is the newer input: the full image reference. Callers
# who need a non-quay source (e.g. PR builds pulling their own
# ghcr.io/kairos-io/kairos/kairos-init:pr-<N>) pass KAIROS_INIT_IMAGE
# directly; everyone else keeps passing KAIROS_INIT (or lets its default
# take effect) and the default of KAIROS_INIT_IMAGE composes the two.
ARG KAIROS_INIT=v0.17.3
ARG KAIROS_INIT_IMAGE=quay.io/kairos/kairos-init:${KAIROS_INIT}

FROM ${KAIROS_INIT_IMAGE} AS kairos-init

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION
ARG FIPS=no-fips

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
    if [ "$FIPS" == "fips" ]; then FIPS_FLAG="--fips"; else FIPS_FLAG=""; fi; \
    eval /kairos-init -l debug -s install -m \"${MODEL}\" -t \"${TRUSTED_BOOT}\" ${K8S_FLAG} ${K8S_VERSION_FLAG} --version \"${VERSION}\" \"${FIPS_FLAG}\" && \
    eval /kairos-init -l debug -s init -m \"${MODEL}\" -t \"${TRUSTED_BOOT}\" ${K8S_FLAG} ${K8S_VERSION_FLAG} --version \"${VERSION}\" \"${FIPS_FLAG}\"
