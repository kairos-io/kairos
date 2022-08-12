#!/bin/bash

go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo@latest
ginkgo --label-filter "$1" --fail-fast -r ./tests/