#!/bin/sh
# EXPERIMENTAL p2p demo. Needs docker with the compose plugin.
set -eu
cd "$(dirname "$0")"
trap 'docker compose down -v >/dev/null 2>&1 || true' EXIT
docker compose down -v >/dev/null 2>&1 || true
docker compose build
echo "== proving the network is isolated (the request must fail) =="
docker compose run --rm --no-deps --entrypoint sh leech1 -c \
  'wget -T 3 -q -O- http://1.1.1.1 >/dev/null 2>&1 && echo "INTERNET REACHABLE?!" || echo "no internet: ok"' | tail -1
echo "== starting 1 seed, a rogue seeder, 8 hostile peers and 5 leechers =="
docker compose up -d
for n in leech1 leech2 leech3 leech4 leech5; do
  until [ "$(docker inspect -f '{{.State.Status}}' kairos-p2p-demo-$n-1 2>/dev/null || echo none)" = exited ]; do sleep 2; done
done
for n in seed rogue stall lie fake1 fake2 fake3 fake4 fake5 fake6 leech1 leech2 leech3 leech4 leech5; do
  echo; echo "================ $n ================"
  docker compose logs --no-log-prefix "$n"
done
echo; for n in leech1 leech2 leech3 leech4 leech5; do
  printf '%s exit code: ' "$n"; docker inspect -f '{{.State.ExitCode}}' kairos-p2p-demo-$n-1
done
