# Stays on 1.26 until kubo builds under 1.27: kubo v0.41.0 pulls in
# cockroachdb/swiss, which go:linknames into runtime internals (hashFn,
# getRuntimeHasher, fastrand64) that 1.27 no longer exposes, so the ipfs
# build below dies with a pile of "undefined:" errors.
FROM golang:1.27 AS build

# Install a binary
RUN git clone -b v0.41.0 https://github.com/ipfs/kubo.git /kubo
WORKDIR /kubo
RUN go mod tidy
RUN go mod download
RUN CGO_ENABLED=0 go build -o ipfs ./cmd/ipfs
WORKDIR /bundle
RUN mkdir -p /bundle/usr/bin
RUN mv /kubo/ipfs /bundle/usr/bin/ipfs
RUN mkdir -p /bundle/usr/lib/extension-release.d/
# systemd version 252+ is necessary in order to use the special key _any here
RUN echo ID=_any > /bundle/usr/lib/extension-release.d/extension-release.kubo

FROM scratch

COPY --from=build /bundle/usr/bin/ipfs /usr/bin/ipfs
COPY --from=build /bundle/usr/lib/extension-release.d /usr/lib/extension-release.d
