FROM alpine@sha256:1e42bbe2508154c9126d48c2b8a75420c3544343bf86fd041fb7527e017a4b4a as build

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
