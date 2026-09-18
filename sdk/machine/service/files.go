package service

import (
	"os"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/sdk/utils"
)

// writeFile writes content, creating the parent directory when it is missing.
// Writing under a Spec Root usually means writing into a tree that is still
// being built, where /etc/init.d may not exist yet.
func writeFile(path, content string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), perm)
}

// setEnv merges values into an env file, creating its directory when missing.
func setEnv(path string, env map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return utils.WriteEnv(path, env)
}
