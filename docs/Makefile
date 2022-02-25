export HUGO_VERSION?=0.91.2
export HUGO_PLATFORM?=Linux-64bit

export ROOT_DIR:=$(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))/

.DEFAULT_GOAL := build

.PHONY: build
build:
	scripts/build.sh
	cp -rf CNAME public/

.PHONY: serve
serve:
	scripts/serve.sh

.PHONY: publish
publish:
	scripts/publish.sh
