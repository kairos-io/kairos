# Container image for the in-cluster kcrypt-challenger server.
#
# Consumes the kcrypt-challenger binary that `make binaries` produced at
# dist/linux-<arch>/kcrypt-challenger. Multi-arch builds are handled by
# `docker buildx build --platform=linux/amd64,linux/arm64` in the release
# workflow, which sets TARGETARCH per platform and picks up the right
# binary from the build context.
# nonroot, resolved 2026-09-04
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
ARG TARGETARCH
COPY dist/linux-${TARGETARCH}/kcrypt-challenger /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
