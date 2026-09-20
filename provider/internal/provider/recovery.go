package provider

import (
	"fmt"
	"os"

	"github.com/kairos-io/kairos/v4/sdk/utils"
	strutils "github.com/kairos-io/kairos/v4/sdk/utils/strings"

	nodepair "github.com/kairos-io/go-nodepair"
	"github.com/mudler/go-pluggable"
	process "github.com/mudler/go-processmanager"
)

const recoveryAddr = "127.0.0.1:2222"
const sshStateDir = "/tmp/.ssh_recovery"

// recoveryServerEnv is the environment the recovery-ssh-server command is
// started with. Every name here has to be one that command declares a flag
// for, otherwise the value is dropped without a word.
//
// The token goes in as EDGEVPNTOKEN rather than TOKEN: the flag that carries
// it is EdgeVPN's own, out of cmd.CommonFlags, which the command appends so
// that cmd.ConfigFromContext can read a whole network config.
func recoveryServerEnv(token, serviceUUID, password, listen string) []string {
	return []string{
		fmt.Sprintf("EDGEVPNTOKEN=%s", token),
		fmt.Sprintf("SERVICE=%s", serviceUUID),
		fmt.Sprintf("LISTEN=%s", listen),
		fmt.Sprintf("PASSWORD=%s", password),
	}
}

func Recovery(e *pluggable.Event) pluggable.EventResponse { //nolint:revive

	resp := &pluggable.EventResponse{}

	tk := nodepair.GenerateToken()

	serviceUUID := strutils.RandStringRunes(10)
	generatedPassword := strutils.RandStringRunes(7)
	resp.Data = utils.EncodeRecoveryToken(tk, serviceUUID, generatedPassword)
	resp.State = fmt.Sprintf(
		"starting ssh server on '%s', password: '%s' service: '%s' ", recoveryAddr, generatedPassword, serviceUUID)

	// start ssh server in a separate process

	sshServer := process.New(
		process.WithName(os.Args[0]),
		process.WithArgs("recovery-ssh-server"),
		process.WithEnvironment(recoveryServerEnv(tk, serviceUUID, generatedPassword, recoveryAddr)...),
		process.WithStateDir(sshStateDir),
	)

	err := sshServer.Run()
	if err != nil {
		resp.Error = err.Error()
	}
	return *resp
}

func RecoveryStop(e *pluggable.Event) pluggable.EventResponse { //nolint:revive
	resp := &pluggable.EventResponse{}

	sshServer := process.New(
		process.WithStateDir(sshStateDir),
	)

	err := sshServer.Stop()
	if err != nil {
		resp.Error = err.Error()
	} else {
		os.RemoveAll(sshStateDir)
	}
	return *resp
}
