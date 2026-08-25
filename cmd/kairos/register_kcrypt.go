package main

import (
	kcrypt "github.com/kairos-io/kairos/v4/kcrypt/discovery"
)

func init() {
	// "kcrypt-discovery-challenger" is the historical binary name used by
	// symlink invocation and existing scripts; it is registered as an alias
	// so `kairos kcrypt`, `kairos kcrypt-discovery-challenger`, and the
	// symlink `./kcrypt-discovery-challenger` all resolve to the same code.
	register("kcrypt", kcrypt.Run, "kcrypt-discovery-challenger")
}
