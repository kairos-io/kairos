#!/bin/bash
set -e

: ${BINARY_NAME:="c3os"}
: ${INSTALL_DIR:="/usr/local/bin"}
: ${USE_SUDO:="true"}
: ${INSTALL_K3S:="true"}
: ${INSTALL_EDGEVPN:="true"}
: ${DOWNLOADER:="curl"}

install_k3s() {
    export INSTALL_K3S_VERSION=${K3S_VERSION}
    export INSTALL_K3S_BIN_DIR="/usr/bin"
    export INSTALL_K3S_SKIP_START="true" 
    export INSTALL_K3S_SKIP_ENABLE="true" 
    curl -sfL https://get.k3s.io | sh -
}

c3os_github_version() {
    set +e
    curl -s https://api.github.com/repos/mudler/c3os/releases/latest | \
    grep tag_name | \
    awk '{ print $2 }' | \
    sed -e 's/\"//g' -e 's/,//g' || echo "v0.8.5"
    set -e
}

install_edgevpn() {
    curl -sfL https://raw.githubusercontent.com/mudler/edgevpn/master/install.sh | sh -
}

download() {
    [ $# -eq 2 ] || fatal 'download needs exactly 2 arguments'

    case $DOWNLOADER in
        curl)
            curl -o $1 -sfL $2
            ;;
        wget)
            wget -qO $1 $2
            ;;
        *)
            fatal "Incorrect executable '$DOWNLOADER'"
            ;;
    esac

    # Abort if download command failed
    [ $? -eq 0 ] || fatal 'Download failed'
}

SUDO=sudo
if [ $(id -u) -eq 0 ]; then
    SUDO=
fi

c3os_version="${C3OS_VERSION:-$(c3os_github_version)}"

echo "Downloading c3os $c3os_version"

if [ "${INSTALL_EDGEVPN}" == "true" ]; then
    install_edgevpn
fi

if [ "${INSTALL_K3S}" == "true" ]; then
    install_k3s
fi

TMP_DIR=$(mktemp -d -t c3os-install.XXXXXXXXXX)

download $TMP_DIR/out.tar.gz  https://github.com/c3os-io/c3os/releases/download/$c3os_version/c3os-$c3os_version-Linux-x86_64.tar.gz

# TODO verify w/ checksum
tar xvf $TMP_DIR/out.tar.gz -C $TMP_DIR

$SUDO cp -rf $TMP_DIR/c3os $INSTALL_DIR/

# TODO trap
rm -rf $TMP_DIR

if [ ! -d "/etc/systemd/system.conf.d/" ]; then
    $SUDO mkdir -p /etc/systemd/system.conf.d
fi

if [ ! -d "/etc/sysconfig" ]; then
    $SUDO mkdir -p /etc/sysconfig
fi