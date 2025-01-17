ARG BASE_IMAGE=ubuntu:24.04

FROM ttl.sh/kairos-init:main AS kairos-init

FROM ${BASE_IMAGE}
ARG MODEL=generic
ARG VARIANT=core
ARG TRUSTED_BOOT=false

COPY --from=kairos-init /kairos-init /kairos-init
RUN /kairos-init -l debug -s install -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}"
RUN /kairos-init -l debug -s init -m "${MODEL}" -v "${VARIANT}" -t "${TRUSTED_BOOT}"
RUN rm /kairos-init
