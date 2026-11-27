# Demo image: kairos-agent (the p2p command is part of it), on a tiny base.
FROM golang:1.27.4 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /kairos-agent ./agent && CGO_ENABLED=0 go build -ldflags "-s -w" -o /p2p-evil ./agent/hack/p2p-demo/evil

FROM alpine:3.22
RUN apk add --no-cache coreutils iproute2 && mkdir -p /shared /data && chmod 777 /shared /data
COPY --from=build /kairos-agent /usr/local/bin/kairos-agent
COPY --from=build /p2p-evil /usr/local/bin/p2p-evil
COPY agent/hack/p2p-demo/node.sh /usr/local/bin/node.sh
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/node.sh"]
