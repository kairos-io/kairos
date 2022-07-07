#!/bin/bash
set -ex

export CGO_ENABLED=0

apt-get update && apt-get install -y upx

go build -ldflags "-s -w" -o c3os ./cmd/cli && upx c3os
go build -ldflags "-s -w" -o c3os-agent ./cmd/agent && upx c3os-agent
go build -ldflags "-s -w" -o agent-provider-c3os ./cmd/provider && upx agent-provider-c3os