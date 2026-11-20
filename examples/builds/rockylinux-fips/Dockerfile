FROM rockylinux:9@sha256:03303863808e9cd79f0c58c833db3e5f9cde9f2abe16804a267b05dc54af86e4
ARG VERSION=v0.0.1

RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.5.0,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s install --fips --version "${VERSION}"

RUN --mount=type=bind,from=quay.io/kairos/kairos-init:v4.5.0,src=/kairos-init,dst=/kairos-init \
    /kairos-init -l debug -s init --fips --version "${VERSION}"

# Symlink kernel HMAC
RUN kernel=$(ls /boot/vmlinuz-* | head -n1) && ln -sf ."${kernel#/boot/}".hmac /boot/.vmlinuz.hmac
