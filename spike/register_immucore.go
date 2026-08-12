package main

import (
	immucore "github.com/kairos-io/immucore/pkg/cmd"
)

func init() {
	register("immucore", immucore.Run)
}
