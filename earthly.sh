#!/bin/bash

docker run --privileged -v /var/run/docker.sock:/var/run/docker.sock --rm -t -v $(pwd):/workspace -v earthly-tmp:/tmp/earthly:rw earthly/earthly:v0.6.21 --allow-privileged $@