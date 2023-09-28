#!/bin/bash

docker run --privileged -v /var/run/docker.sock:/var/run/docker.sock --rm --env EARTHLY_BUILD_ARGS -t -v "$(pwd)":/workspace -v earthly-tmp:/tmp/earthly:rw earthly/earthly:v0.7.19 --allow-privileged "$@"
