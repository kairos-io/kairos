ARG BASE_IMAGE=ubuntu:20.04
ARG KAIROS_INIT=v0.14.6

# TEST OVERRIDE: build immucore from a branch instead of the kairos-init pinned release.
# Remove this stage + the override RUN below to go back to the released immucore.
ARG IMMUCORE_REPO=https://github.com/kairos-io/immucore
ARG IMMUCORE_BRANCH=fix/uki-sysroot-perms-0755

FROM quay.io/kairos/kairos-init:${KAIROS_INIT} AS kairos-init

FROM golang:1.26 AS immucore-build
ARG IMMUCORE_REPO
ARG IMMUCORE_BRANCH
RUN git clone --depth 1 --branch "${IMMUCORE_BRANCH}" "${IMMUCORE_REPO}" /src
WORKDIR /src
RUN CGO_ENABLED=0 go build -tags "nogit nounpack" \
    -ldflags "-w -s -X github.com/kairos-io/immucore/internal/version.version=${IMMUCORE_BRANCH}" \
    -o /immucore .

FROM ${BASE_IMAGE} AS base-kairos
ARG MODEL=generic
ARG TRUSTED_BOOT=false
ARG KUBERNETES_DISTRO
ARG KUBERNETES_VERSION
ARG VERSION
ARG FIPS=no-fips

RUN --mount=type=bind,from=kairos-init,src=/kairos-init,dst=/kairos-init \
    if [ -n "${KUBERNETES_DISTRO}" ]; then \
        K8S_FLAG="-p ${KUBERNETES_DISTRO}"; \
        if [ "${KUBERNETES_DISTRO}" = "k0s" ] && [ -n "${KUBERNETES_VERSION}" ]; then \
            K8S_VERSION_FLAG="--provider-k0s-version \"${KUBERNETES_VERSION}\""; \
        elif [ "${KUBERNETES_DISTRO}" = "k3s" ] && [ -n "${KUBERNETES_VERSION}" ]; then \
            K8S_VERSION_FLAG="--provider-k3s-version \"${KUBERNETES_VERSION}\""; \
        else \
            K8S_VERSION_FLAG=""; \
        fi; \
    else \
        K8S_FLAG=""; \
        K8S_VERSION_FLAG=""; \
    fi; \
    if [ "$FIPS" == "fips" ]; then FIPS_FLAG="--fips"; else FIPS_FLAG=""; fi; \
    eval /kairos-init -l debug -s install -m \"${MODEL}\" -t \"${TRUSTED_BOOT}\" ${K8S_FLAG} ${K8S_VERSION_FLAG} --version \"${VERSION}\" \"${FIPS_FLAG}\" && \
    eval /kairos-init -l debug -s init -m \"${MODEL}\" -t \"${TRUSTED_BOOT}\" ${K8S_FLAG} ${K8S_VERSION_FLAG} --version \"${VERSION}\" \"${FIPS_FLAG}\"

# TEST OVERRIDE: replace the installed immucore with the branch build.
# Resolves the real installed path (no hardcoding) and fails the build if immucore
# was not installed or the new binary does not run. The UKI initramfs is regenerated
# from this rootfs by auroraboot build-uki, so the replaced binary is what gets booted.
RUN --mount=type=bind,from=immucore-build,src=/immucore,dst=/tmp/immucore \
    dst="$(command -v immucore)"; \
    [ -n "$dst" ] || { echo "immucore not found in image - kairos-init did not install it"; exit 1; }; \
    echo "Overriding immucore at: $dst"; \
    install -m 0755 /tmp/immucore "$dst"; \
    immucore version
