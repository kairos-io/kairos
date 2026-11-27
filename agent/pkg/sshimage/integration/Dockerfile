FROM alpine:3.23
RUN apk add --no-cache openssh-server openssh-client && adduser -D -u 1000 it
COPY sshimage.test /usr/local/bin/sshimage.test
