FROM quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.2.1 AS base
ARG TARGETARCH
COPY luet.yaml /etc/luet/luet.yaml
RUN luet install -y --relax utils/keylime-agent