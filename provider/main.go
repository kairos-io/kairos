package main

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos/v4/sdk/bus"
	"github.com/kairos-io/provider-kairos/v2/internal/cli"
	"github.com/kairos-io/provider-kairos/v2/internal/provider"
)

// checkErr reports err on stderr, never on stdout, because stdout carries data
// in both of this binary's modes and must not have diagnostics mixed into it.
//
// Run as a plugin, go-pluggable writes its JSON response to stdout, so anything
// printed here would be appended after that response and corrupt the stream the
// agent parses. Run as a CLI, stdout is the answer itself: get-kubeconfig is
// routinely redirected into a file, and an error printed there would end up
// inside the kubeconfig.
//
// go-pluggable does capture whatever an event handler prints to stdout and
// stderr while the handler runs, and attaches it to the response as Logs. This
// function runs after that capture has been torn down, so it is outside it.
func checkErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func main() {
	if len(os.Args) >= 2 && bus.IsEventDefined(os.Args[1], "init.provider.info") {
		checkErr(provider.Start())
	}

	checkErr(cli.Start())
}
