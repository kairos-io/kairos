package utils

import (
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/joho/godotenv"
)

func SH(c string) (string, error) {
	o, err := exec.Command("/bin/sh", "-c", c).CombinedOutput()
	return string(o), err
}

func WriteEnv(envFile string, config map[string]string) error {
	content, _ := ioutil.ReadFile(envFile)
	env, _ := godotenv.Unmarshal(string(content))

	for key, val := range config {
		env[key] = val
	}

	return godotenv.Write(env, envFile)
}

func Shell() *exec.Cmd {
	cmd := exec.Command("/bin/sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd
}
