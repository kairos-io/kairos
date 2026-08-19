package main

import (
	kcrypt "github.com/kairos-io/kairos/kcrypt/discovery"
)

func init() {
	register("kcrypt-discovery-challenger", kcrypt.Run)
}
