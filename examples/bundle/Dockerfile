FROM alpine@sha256:b89d9c93e9ed3597455c90a0b88a8bbb5cb7188438f70953fede212a0c4394e0 as build

# Install a binary
RUN wget https://github.com/ipfs/kubo/releases/download/v0.15.0/kubo_v0.15.0_linux-amd64.tar.gz -O kubo.tar.gz
RUN tar xvf kubo.tar.gz
RUN mv kubo/ipfs /usr/bin/ipfs
RUN mkdir -p /usr/lib/extension-release.d/
# systemd version 252+ is necessary in order to use the special key _any here
RUN echo ID=_any > /usr/lib/extension-release.d/extension-release.kubo

FROM scratch

COPY --from=build /usr/bin/ipfs /usr/bin/ipfs
COPY --from=build /usr/lib/extension-release.d /usr/lib/extension-release.d
