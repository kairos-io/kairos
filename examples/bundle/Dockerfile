FROM alpine as build
# Install a binary
ARG VERSION
ENV VERSION=$VERSION

RUN wget https://github.com/ipfs/kubo/releases/download/v0.15.0/kubo_v0.15.0_linux-amd64.tar.gz -O kubo.tar.gz
RUN tar xvf kubo.tar.gz
RUN mv kubo/ipfs /usr/bin/ipfs
RUN mkdir -p /usr/lib/extension-release.d/
RUN echo KAIROS_ID=kairos > /usr/lib/extension-release.d/extension-release.kubo
RUN echo KAIROS_VERSION_ID=$VERSION >> /usr/lib/extension-release.d/extension-release.kubo


FROM scratch

COPY --from=build /usr/bin/ipfs /usr/bin/ipfs
COPY --from=build /usr/lib/extension-release.d /usr/lib/extension-release.d