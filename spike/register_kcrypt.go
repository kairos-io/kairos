package main

import (
	kcrypt "github.com/kairos-io/kairos-challenger/pkg/discovery"
)

func init() {
	register("kcrypt-discovery-challenger", kcrypt.Run)
}
