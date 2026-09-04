# Container image for the in-cluster kcrypt-challenger server.
#
# Consumes the kcrypt-challenger binary that `make binaries` produced at
# dist/linux-<arch>/kcrypt-challenger. Multi-arch builds are handled by
# `docker buildx build --platform=linux/amd64,linux/arm64` in the release
# workflow, which sets TARGETARCH per platform and picks up the right
# binary from the build context.
# nonroot, resolved 2026-09-04
FROM gcr.io/distroless/static:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7
WORKDIR /
ARG TARGETARCH
COPY dist/linux-${TARGETARCH}/kcrypt-challenger /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
