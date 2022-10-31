#!/bin/bash

go get github.com/onsi/gomega/...
go get github.com/onsi/ginkgo/v2/ginkgo/internal@v2.1.4
go get github.com/onsi/ginkgo/v2/ginkgo/generators@v2.1.4
go get github.com/onsi/ginkgo/v2/ginkgo/labels@v2.1.4
go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo
ginkgo --label-filter "$1" --fail-fast -r ./tests/