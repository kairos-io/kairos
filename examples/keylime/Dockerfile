FROM quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.2.1 AS base
ARG TARGETARCH
# copy both arches
COPY luet-arm64.yaml /tmp/luet-arm64.yaml
COPY luet-amd64.yaml /tmp/luet-amd64.yaml
# Set the default luet config to the current build arch
RUN mkdir -p /etc/luet/
RUN cp /tmp/luet-${TARGETARCH}.yaml /etc/luet/luet.yaml
RUN luet install -y --relax utils/keylime-agent