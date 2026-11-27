#!/bin/sh
# Two containers on a private network: one runs a plain OpenSSH sshd with a
# fake Kairos state partition on it (no forced command, no kairos-agent), the
# other runs the SSH image client tests against it. No QEMU, no registry.
#
# Usage: run.sh <sshimage.test binary>
#   sshimage.test: CGO_ENABLED=0 go test -c -tags integration -o sshimage.test ./agent/pkg/sshimage
set -eu
testbin=$(readlink -f "$1")
here=$(dirname "$(readlink -f "$0")")
work=$(mktemp -d "${TMPDIR:-.}/ssh-image-it.XXXXXX")
net=kairos-ssh-it-net
img=kairos-ssh-it
srv=kairos-ssh-it-server
cleanup() {
	docker rm -f "$srv" >/dev/null 2>&1 || true
	docker network rm "$net" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap cleanup EXIT

cp "$testbin" "$here/Dockerfile" "$work/"
docker build -q -t "$img" "$work" >/dev/null

# keys are created as the calling user, the client container runs as that user too
keygen() { docker run --rm -u "$(id -u):$(id -g)" -v "$work:/w" "$img" ssh-keygen -q -t ed25519 -N "" -f "/w/$1"; }
keygen client_key
keygen host_key
keygen other_host_key
chmod 600 "$work/client_key"

fp() { docker run --rm -u "$(id -u):$(id -g)" -v "$work:/w" "$img" ssh-keygen -l -E sha256 -f "/w/$1" | cut -d" " -f2; }
fingerprint=$(fp host_key.pub)
wrong_fingerprint=$(fp other_host_key.pub)
echo "kairos-ssh-it-server $(cut -d' ' -f1,2 "$work/host_key.pub")" >"$work/known_hosts"
echo "kairos-ssh-it-server $(cut -d' ' -f1,2 "$work/other_host_key.pub")" >"$work/wrong_known_hosts"
cat "$work/client_key.pub" >"$work/authorized_keys"
echo "root=LABEL=COS_ACTIVE cos-img/filename=/cOS/active.img" >"$work/cmdline_active"
echo "root=LABEL=COS_ACTIVE cos-img/filename=/cOS/passive.img" >"$work/cmdline_passive"
echo "root=LABEL=COS_RECOVERY cos-img/filename=/cOS/recovery.img" >"$work/cmdline_recovery"
cat >"$work/sshd_config" <<CFG
HostKey /w/host_key
AuthorizedKeysFile /w/authorized_keys
PermitRootLogin prohibit-password
PasswordAuthentication no
StrictModes no
PidFile none
CFG

docker network create "$net" >/dev/null
docker run -d --name "$srv" --network "$net" --network-alias kairos-ssh-it-server -v "$work:/w:ro" "$img" sh -c '
	mkdir -p /run/initramfs/cos-state/cOS
	for n in active passive; do
		f=/run/initramfs/cos-state/cOS/$n.img
		head -c 6291456 /dev/urandom >$f
		head -c 20971520 /dev/zero >>$f
		head -c 6291456 /dev/urandom >>$f
	done
	ssh-keygen -A >/dev/null 2>&1
	echo root:$(head -c 24 /dev/urandom | base64) | chpasswd >/dev/null 2>&1 || true
	exec /usr/sbin/sshd -D -e -f /w/sshd_config' >/dev/null
# wait for sshd
i=0
until docker exec "$srv" sh -c 'nc -z 127.0.0.1 22' >/dev/null 2>&1; do
	i=$((i + 1))
	[ $i -gt 40 ] && { docker logs "$srv"; echo "sshd did not start"; exit 1; }
	sleep 0.5
done
sha_active=$(docker exec "$srv" sha256sum /run/initramfs/cos-state/cOS/active.img | cut -d' ' -f1)
sha_passive=$(docker exec "$srv" sha256sum /run/initramfs/cos-state/cOS/passive.img | cut -d' ' -f1)
echo "server image sha256: active $sha_active passive $sha_passive"

docker run --rm --network "$net" -u "$(id -u):$(id -g)" -v "$work:/w:ro" \
	-e IT_HOST=kairos-ssh-it-server -e IT_KEY=/w/client_key -e IT_KNOWN_HOSTS=/w/known_hosts \
	-e IT_WRONG_KNOWN_HOSTS=/w/wrong_known_hosts -e IT_FINGERPRINT="$fingerprint" -e IT_WRONG_FINGERPRINT="$wrong_fingerprint" -e IT_SHA256_ACTIVE="$sha_active" -e IT_SHA256_PASSIVE="$sha_passive" \
	"$img" sshimage.test -ginkgo.v
