# Container image for the in-cluster kcrypt-challenger server.
#
# Consumes the kcrypt-challenger binary that `make binaries` produced at
# dist/linux-<arch>/kcrypt-challenger. Multi-arch builds are handled by
# `docker buildx build --platform=linux/amd64,linux/arm64` in the release
# workflow, which sets TARGETARCH per platform and picks up the right
# binary from the build context.
# nonroot, resolved 2026-09-04
FROM gcr.io/distroless/static:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88
WORKDIR /
ARG TARGETARCH
COPY dist/linux-${TARGETARCH}/kcrypt-challenger /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
