package machine

import "os"

func Exists(path string) bool {
	_, err := os.Stat(path)

	return !os.IsNotExist(err)
}
