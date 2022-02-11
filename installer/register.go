package main

import (
	"context"
	"fmt"
	"io/ioutil"

	nodepair "github.com/mudler/go-nodepair"
	qr "github.com/mudler/go-nodepair/qrcode"
)

func register(arg, configFile, device string, reboot, poweroff bool) error {
	b, _ := ioutil.ReadFile(configFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// dmesg -D to suppress tty ev
	fmt.Println("Sending registration payload, please wait")

	config := map[string]string{
		"device": device,
		"cc":     string(b),
	}

	if reboot {
		config["reboot"] = ""
	}

	if poweroff {
		config["poweroff"] = ""
	}

	err := nodepair.Send(
		ctx,
		config,
		nodepair.WithReader(qr.Reader),
		nodepair.WithToken(arg),
	)
	if err != nil {
		return err
	}

	fmt.Println("Payload sent, installation will start on the machine briefly")
	return nil
}
