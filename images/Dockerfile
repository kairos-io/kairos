ARG BASE_IMAGE=ubuntu:24.04
ARG VARIANT=core
ARG MODEL=generic
ARG TRUSTED_BOOT=false


FROM quay.io/kairos/kairos-init:v0.1.1 AS kairos-init
FROM quay.io/luet/base:0.35.5 AS luet

FROM ${BASE_IMAGE} AS base
ARG VARIANT
ARG MODEL
ARG TRUSTED_BOOT
COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}"
RUN /kairos-init -l debug -s init -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}"
#RUN /kairos-init -l debug --validate -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}"
RUN rm /kairos-init


FROM base AS core

FROM base AS standard
COPY --from=luet /usr/bin/luet /usr/bin/luet
RUN luet install -y k8s/k3s-$(grep -q alpine /etc/os-release && echo "openrc" || echo "systemd")
RUN luet install -y system/provider-kairos


FROM ${VARIANT} AS final
