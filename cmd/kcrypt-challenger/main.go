// Command kcrypt-challenger is the in-cluster kcrypt challenger server.
// It runs as a kubernetes controller manager and is deployed as its own
// container image, distinct from the multi-call kairos binary.
package main

import (
	"os"

	"github.com/kairos-io/kairos/kcrypt/challenger"
)

func main() {
	os.Exit(challenger.Run())
}
