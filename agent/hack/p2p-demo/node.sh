#!/bin/sh
# Demo node entrypoint. ROLE=seed|rogue|stall|lie|leech. Everything is local to the closed
# docker network: no tracker, no internet, no keys.
#   MODE=mdns    p2p://active?pin=<hex>                peers found with mDNS + the pin
#   MODE=static  p2p://ip1,ip2,.../active?pin=<hex>    explicit list + the pin
#   MODE=copied  the URI the seed printed, as it is
#   MODE=refuse  p2p://active                          no pin: refused; then --insecure: allowed, loudly
#   MODE=hostile a static list with a stalling peer, a size-lying peer and six fakes that
#                declare the TRUE size and image hash FIRST, then the seed, with the pin
set -eu
ME=$(ip -4 -o addr show eth0 | awk '{print $4}' | cut -d/ -f1)
export KAIROS_P2P_EXPERIMENTAL=1

case "$ROLE" in
seed)
  mkdir -p /data/state/cOS
  echo "[$NAME] creating ${IMAGE_MB:-256} MiB active.img and a 16 MiB passive.img"
  head -c "$(( ${IMAGE_MB:-256} * 1024 * 1024 ))" /dev/urandom > /data/state/cOS/active.img
  head -c "$(( 16 * 1024 * 1024 ))" /dev/urandom > /data/state/cOS/passive.img
  # no --bind, --allow-cidr or per-image arguments: bind is the eth0 address,
  # the allowlist the local subnet, all existing images are served. The seeder
  # prints the sources, with their pins, to use on the other nodes: in real life
  # you copy that line into the cloud-config of the nodes. Here a shared volume does.
  kairos-agent p2p seed --state-dir /data/state --recovery-dir /nonexistent \
    --cache-dir /data/cache --upload-limit "${UPLOAD_KIB:-12288}" | tee /data/seed.out &
  until grep -q "active?pin" /data/seed.out 2>/dev/null; do sleep 1; done
  grep "active?pin" /data/seed.out | tr -d ' ' > /shared/uri
  sed -n 's/.*pin=\([0-9a-f]*\).*/\1/p' /shared/uri > /shared/pin
  # the sha256 of the image, for the hostile peers to claim (they are not given the pin)
  sha256sum /data/state/cOS/active.img | cut -d' ' -f1 > /shared/sha256
  wait
  ;;
rogue)
  # a host on the network that announces itself and serves ANOTHER image
  mkdir -p /data/state/cOS
  head -c "$(( 32 * 1024 * 1024 ))" /dev/urandom > /data/state/cOS/active.img
  echo "[$NAME] ($ME) rogue seeder: a different active.img"
  exec kairos-agent p2p seed --state-dir /data/state --recovery-dir /nonexistent --cache-dir /data/cache
  ;;
stall|lie|fake)
  # hostile peers: they announce the image hash, then stall, claim a size the disk cannot
  # hold, or (fake) declare the true size and hash with chunk hashes of their own content
  while [ ! -s /shared/sha256 ]; do sleep 1; done
  exec p2p-evil "$ROLE" "$(cat /shared/sha256)"
  ;;
leech)
  while [ ! -s /shared/uri ]; do sleep 1; done
  PIN=$(cat /shared/pin)
  PEERS=""
  for p in $PEER_IPS; do [ "$p" = "$ME" ] || PEERS="$PEERS${PEERS:+,}$p"; done
  ARGS=""
  case "$MODE" in
  static) SRC="p2p://$PEERS/active?pin=$PIN" ;;
  mdns)   SRC="p2p://active?pin=$PIN" ;;
  copied) SRC=$(cat /shared/uri) ;;
  refuse) SRC="p2p://active" ;;
  hostile)
    # the hostile peers must be up before we start, or they would simply be unreachable
    for h in 16 17 20 21 22 23 24 25; do until wget -q -T 2 -O /dev/null http://172.28.0.$h:42069/active/manifest; do sleep 1; done; done
    # eight hostile addresses, listed first: none of their manifests is the pinned one, so none costs a chunk
    SRC="p2p://172.28.0.16,172.28.0.17,172.28.0.20,172.28.0.21,172.28.0.22,172.28.0.23,172.28.0.24,172.28.0.25,172.28.0.10/active?pin=$PIN" ;;
  esac
  echo "[$NAME] ($ME) fetching $SRC"
  fetch() { # sets rc
    rc=0
    kairos-agent p2p fetch --port 42069 --upload-limit "${UPLOAD_KIB:-12288}" $ARGS \
      --source "$SRC" --output /data/active.img --linger "${LINGER:-25s}" > /data/fetch.log 2>&1 || rc=$?
    grep -v '^EXPERIMENTAL' /data/fetch.log
  }
  if [ "$MODE" = refuse ]; then
    # no pin: the node refuses before it talks to anyone
    fetch
    if [ $rc -eq 0 ]; then echo "[$NAME] ERROR: it did not refuse"; exit 1; fi
    echo "[$NAME] refused, as it should (exit code $rc)"
    echo "[$NAME] now with the explicit --insecure opt in:"
    ARGS="--insecure"
  fi
  i=0
  # the seed may still be creating/hashing its image: retry for a while
  while :; do
    fetch
    [ $rc -eq 0 ] && break
    i=$((i+1)); [ $i -ge 20 ] && { echo "[$NAME] giving up"; exit 1; }
    echo "[$NAME] attempt $i failed, retrying in 3s"; sleep 3
  done
  sha256sum /data/active.img | sed "s/^/[$NAME] on disk /"
  ;;
esac
