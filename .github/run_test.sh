#!/bin/bash

go get github.com/onsi/ginkgo/v2
go get github.com/onsi/gomega/...
go get github.com/onsi/ginkgo/v2/ginkgo/internal@v2.1.3
go install github.com/onsi/ginkgo/v2/ginkgo
ginkgo --label-filter "$1" --fail-fast -r ./tests/