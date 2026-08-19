# Build stage runs on the host's native platform (BUILDPLATFORM) to avoid QEMU emulation overhead.
# BUILDPLATFORM = the machine running docker buildx (e.g. linux/amd64)
# TARGETPLATFORM = the platform we're producing the image for (e.g. linux/riscv64)
# BUILDARCH / TARGETARCH = the arch component of each (e.g. amd64, riscv64)
# These are set automatically by buildx; no extra flags needed when invoking docker build.
FROM --platform=$BUILDPLATFORM golang AS build
# TARGETARCH: arch we're building FOR (used to cross-compile Go and download bundled binaries)
ARG TARGETARCH
# BUILDARCH: arch we're building ON (used to download tools like UPX that run during build)
ARG BUILDARCH
# Set SKIP_UPX=1 when bundled binaries must not be UPX-compressed (e.g. linux/riscv64); CI passes this as build-arg.
ARG SKIP_UPX
ENV SKIP_UPX=${SKIP_UPX}
WORKDIR /app
# Install UPX for BUILDARCH — it runs on the build machine to compress TARGETARCH binaries
RUN apt-get update && apt-get install -y --no-install-recommends xz-utils file && \
  curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 https://github.com/upx/upx/releases/download/v5.1.1/upx-5.1.1-${BUILDARCH}_linux.tar.xz -o - | tar xvJf - -C /tmp && \
  cp /tmp/upx-5.1.1-${BUILDARCH}_linux/upx /usr/local/bin/ && \
  chmod +x /usr/local/bin/upx && \
  apt-get remove -y xz-utils && \
  rm -rf /var/lib/apt/lists/*
# Copy dependency files first to leverage Docker cache
COPY Makefile go.mod go.sum ./
RUN go mod download
# Copy source (pkg/bundled/binaries/ excluded via .dockerignore)
COPY . .
# Download and compress bundled binaries for TARGETARCH (runs natively on BUILDARCH)
RUN ARCH=${TARGETARCH} make all
ENV CGO_ENABLED=0
RUN echo "Building version: $(git describe --tags --always --dirty)"
RUN echo "Building commit: $(git rev-parse --short HEAD)"
# Cross-compile for TARGETARCH — runs natively on BUILDARCH, no emulation needed
RUN GOOS=linux GOARCH=${TARGETARCH} go build -o /app/kairos-init --ldflags "-w -s -X github.com/kairos-io/kairos-init/pkg/values.version=$(git describe --tags --always --dirty) -X github.com/kairos-io/kairos-init/pkg/values.gitCommit=$(git rev-parse --short HEAD)"


FROM scratch
COPY --from=build /app/kairos-init /kairos-init
ENTRYPOINT ["/kairos-init"]
