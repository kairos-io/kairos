// Command discovery is the on-device kcrypt discovery client (also known by
// its binary name kcrypt-discovery-challenger). It builds standalone; the
// same code is dispatched from the multi-call kairos binary via argv[0].
package main

import (
	"os"

	"github.com/kairos-io/kairos/v4/kcrypt/discovery"
)

func main() {
	os.Exit(discovery.Run())
}
