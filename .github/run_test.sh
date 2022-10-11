#!/bin/bash

go install github.com/onsi/ginkgo/v2/ginkgo
ginkgo --label-filter "$1" --fail-fast -r ./tests/