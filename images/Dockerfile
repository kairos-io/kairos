ARG BASE_IMAGE=ubuntu:20.04
ARG KAIROS_INIT=v0.5.10

FROM quay.io/kairos/kairos-init:${KAIROS_INIT} AS kairos-init

FROM --platform=$BUILDPLATFORM golang:1.24.4 AS provider-kairos-builder
ENV GOTOOLCHAIN=auto
ARG TARGETOS
ARG TARGETARCH
ARG PROVIDER_KAIROS_REF=disable-k8s
WORKDIR /src
RUN git clone --depth 1 --branch "${PROVIDER_KAIROS_REF}" https://github.com/kairos-io/provider-kairos.git .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/provider-kairos .

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -m "${MODEL}" -t "${TRUSTED_BOOT}" -k "${KUBERNETES_DISTRO}" --k8sversion "${KUBERNETES_VERSION}" --version "${VERSION}" && /kairos-init validate -t "${TRUSTED_BOOT}"

COPY --from=provider-kairos-builder /out/provider-kairos /system/providers/agent-provider-kairos