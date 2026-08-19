# Container image for kairos-init (the OS-image initializer).
#
# Consumes the kairos-init binary that `make binaries` produced at
# dist/linux-<arch>/kairos-init. Multi-arch builds are handled by
# `docker buildx build --platform=linux/amd64,linux/arm64,linux/riscv64`
# in the release workflow, which sets TARGETARCH per platform and picks
# up the right binary from the build context.
#
# The prior in-container build was replaced by this consumer shape when
# kairos-init was absorbed into the kairos monorepo. `make binaries` now
# handles cross-compile and embedded-binaries preparation outside Docker.
FROM scratch
ARG TARGETARCH
COPY dist/linux-${TARGETARCH}/kairos-init /kairos-init
ENTRYPOINT ["/kairos-init"]
