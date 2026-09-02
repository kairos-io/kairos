# kubo v0.41.0's transitive dependency github.com/cockroachdb/swiss only
# supports Go versions in the [1.20, 1.27) range (see runtime_go1.20.go's
# `//go:build (go1.20 && !go1.27) || untested_go_version` constraint), so
# building with Go 1.27+ leaves its runtime hooks undefined and fails.
# Do not bump past golang:1.26.x until that dependency adds Go 1.27+ support.
FROM golang:1.26 AS build

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
