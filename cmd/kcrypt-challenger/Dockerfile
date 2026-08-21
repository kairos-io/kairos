# Container image for the in-cluster kcrypt-challenger server.
#
# Consumes the kcrypt-challenger binary that `make binaries` produced at
# dist/linux-<arch>/kcrypt-challenger. Multi-arch builds are handled by
# `docker buildx build --platform=linux/amd64,linux/arm64` in the release
# workflow, which sets TARGETARCH per platform and picks up the right
# binary from the build context.
FROM gcr.io/distroless/static:nonroot
WORKDIR /
ARG TARGETARCH
COPY dist/linux-${TARGETARCH}/kcrypt-challenger /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
