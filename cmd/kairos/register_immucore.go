package main

import (
	immucore "github.com/kairos-io/kairos/immucore/pkg/cmd"
)

func init() {
	register("immucore", immucore.Run)
}
