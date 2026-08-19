package bus

import (
	"os"
	"os/exec"
)

func RunHookScript(s string) error {
	_, err := os.Stat(s)
	if err != nil {
		return nil
	}
	cmd := exec.Command(s)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
