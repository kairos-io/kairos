package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"

	"github.com/c3os-io/c3os/internal/bus"
	machine "github.com/c3os-io/c3os/internal/machine"
	"github.com/c3os-io/c3os/internal/utils"
	events "github.com/c3os-io/c3os/pkg/bus"
	config "github.com/c3os-io/c3os/pkg/config"
	"github.com/nxadm/tail"
)

// setup needs edgevpn and k3s installed locally
// (both k3s and k3s-agent systemd services)
func agent(apiAddress string, dir []string, force bool) error {

	os.MkdirAll("/usr/local/.c3os", 0600)

	// Reads config
	c, err := config.Scan(config.Directories(dir...))
	if err != nil {
		return err
	}

	// TODO: Proper cleanup the log file
	f, err := ioutil.TempFile(os.TempDir(), "c3os")
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(f.Name(), []byte{}, os.ModePerm)
	if err != nil {
		return err
	}

	t, err := tail.TailFile(f.Name(), tail.Config{Follow: true})
	if err != nil {
		return err
	}

	defer os.RemoveAll(f.Name())

	utils.OnSignal(func() {
		os.RemoveAll(f.Name())
	}, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for line := range t.Lines {
			fmt.Println(line.Text)
		}
	}()

	if !machine.SentinelExist("bundles") {
		opts := c.Bundles.Options()
		err := machine.RunBundles(opts...)
		if !c.IgnoreBundleErrors && err != nil {
			return err
		}

		machine.CreateSentinel("bundles")
	}

	_, err = bus.Manager.Publish(events.EventBootstrap, events.BootstrapPayload{APIAddress: apiAddress, Config: c.String(), Logfile: f.Name()})
	return err
}
