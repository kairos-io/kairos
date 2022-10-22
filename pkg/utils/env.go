package utils

import (
	"os"
	"strings"
)

func SetEnv(env []string) {

	for _, e := range env {
		pair := strings.SplitN(e, "=", 2)
		if len(pair) >= 2 {
			os.Setenv(pair[0], pair[1])
		}
	}
}
