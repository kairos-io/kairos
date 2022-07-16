package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	logging "github.com/ipfs/go-log"
	edgeVPNClient "github.com/mudler/edgevpn/api/client"
	"go.uber.org/zap"

	eventBus "github.com/c3os-io/c3os/internal/bus"
	"github.com/c3os-io/c3os/internal/machine"
	"github.com/c3os-io/c3os/internal/machine/openrc"
	"github.com/c3os-io/c3os/internal/machine/systemd"
	"github.com/c3os-io/c3os/internal/role"
	"github.com/c3os-io/c3os/internal/utils"
	"github.com/c3os-io/c3os/internal/vpn"

	"github.com/c3os-io/c3os/pkg/bus"
	"github.com/c3os-io/c3os/pkg/config"
	"github.com/mudler/edgevpn/api/client/service"
	"github.com/mudler/go-pluggable"
)

func Bootstrap(e *pluggable.Event) pluggable.EventResponse {
	cfg := &bus.BootstrapPayload{}
	err := json.Unmarshal([]byte(e.Data), cfg)
	if err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed reading JSON input: %s input '%s'", err.Error(), e.Data)}
	}

	c := &config.Config{}
	err = config.FromString(cfg.Config, c)
	if err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed reading JSON input: %s input '%s'", err.Error(), cfg.Config)}
	}

	// TODO: this belong to a systemd service that is started instead

	tokenNotDefined := (c.C3OS == nil || c.C3OS.NetworkToken == "")

	if c.C3OS == nil && !c.K3s.Enabled && !c.K3sAgent.Enabled {
		return pluggable.EventResponse{State: "No config file supplied"}
	}

	utils.SH("elemental run-stage c3os-agent.bootstrap")
	eventBus.RunHookScript("/usr/bin/c3os-agent.bootstrap.hook")

	logLevel := "debug"

	if c.C3OS != nil && c.C3OS.LogLevel != "" {
		logLevel = c.C3OS.LogLevel
	}

	lvl, err := logging.LevelFromString(logLevel)
	if err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed setup VPN: %s", err.Error())}
	}

	// TODO: Fixup Logging to file
	loggerCfg := zap.NewProductionConfig()
	loggerCfg.OutputPaths = []string{
		cfg.Logfile,
	}
	logger, err := loggerCfg.Build()
	if err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed setup VPN: %s", err.Error())}
	}

	logging.SetAllLoggers(lvl)

	log := &logging.ZapEventLogger{SugaredLogger: *logger.Sugar()}

	// Do onetimebootstrap if K3s or K3s-agent are enabled.
	// Those blocks are not required to be enabled in case of a c3os
	// full automated setup. Otherwise, they must be explicitly enabled.
	if c.K3s.Enabled || c.K3sAgent.Enabled {
		err := oneTimeBootstrap(log, c, func() error { return vpn.Setup(machine.EdgeVPNDefaultInstance, cfg.APIAddress, "/", true, c) })
		if err != nil {
			return pluggable.EventResponse{Error: fmt.Sprintf("Failed setup: %s", err.Error())}
		}
		return pluggable.EventResponse{}
	} else if tokenNotDefined {
		return pluggable.EventResponse{Error: "No network token provided, exiting"}
	}

	logger.Info("Configuring VPN")

	if err := vpn.Setup(machine.EdgeVPNDefaultInstance, cfg.APIAddress, "/", true, c); err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed setup VPN: %s", err.Error())}
	}

	networkID := "c3os"

	if c.C3OS != nil && c.C3OS.NetworkID != "" {
		networkID = c.C3OS.NetworkID
	}

	cc := service.NewClient(
		networkID,
		edgeVPNClient.NewClient(edgeVPNClient.WithHost(cfg.APIAddress)))

	nodeOpts := []service.Option{
		service.WithLogger(log),
		service.WithClient(cc),
		service.WithUUID(machine.UUID()),
		service.WithStateDir("/usr/local/.c3os/state"),
		service.WithNetworkToken(c.C3OS.NetworkToken),
		service.WithPersistentRoles("auto"),
		service.WithRoles(
			service.RoleKey{
				Role:        "master",
				RoleHandler: role.Master(c),
			},
			service.RoleKey{
				Role:        "worker",
				RoleHandler: role.Worker(c),
			},
			service.RoleKey{
				Role:        "auto",
				RoleHandler: role.Auto(c),
			},
		),
	}

	// Optionally set up a specific node role if the user has defined so
	if c.C3OS.Role != "" {
		nodeOpts = append(nodeOpts, service.WithDefaultRoles(c.C3OS.Role))
	}

	k, err := service.NewNode(nodeOpts...)
	if err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed creating node: %s", err.Error())}
	}
	err = k.Start(context.Background())
	if err != nil {
		return pluggable.EventResponse{Error: fmt.Sprintf("Failed start: %s", err.Error())}
	}

	return pluggable.EventResponse{
		State: "",
		Data:  "",
		Error: "shouldn't return here",
	}
}

func oneTimeBootstrap(l logging.StandardLogger, c *config.Config, vpnSetupFN func() error) error {
	if role.SentinelExist() {
		l.Info("Sentinel exists, nothing to do. exiting.")
		return nil
	}
	l.Info("One time bootstrap starting")

	var svc machine.Service
	k3sConfig := config.K3s{}
	svcName := "k3s"
	svcRole := "server"

	if c.K3s.Enabled {
		k3sConfig = c.K3s
	} else if c.K3sAgent.Enabled {
		k3sConfig = c.K3sAgent
		svcName = "k3s-agent"
		svcRole = "agent"
	}

	if utils.IsOpenRCBased() {
		svc, _ = openrc.NewService(
			openrc.WithName(svcName),
		)
	} else {
		svc, _ = systemd.NewService(
			systemd.WithName(svcName),
		)
	}

	envFile := machine.K3sEnvUnit(svcName)
	if svc == nil {
		return fmt.Errorf("could not detect OS")
	}

	// Setup systemd unit and starts it
	if err := utils.WriteEnv(envFile,
		k3sConfig.Env,
	); err != nil {
		return err
	}

	k3sbin := utils.K3sBin()
	if k3sbin == "" {
		return fmt.Errorf("no k3s binary found (?)")
	}
	if err := svc.OverrideCmd(fmt.Sprintf("%s %s %s", k3sbin, svcRole, strings.Join(k3sConfig.Args, " "))); err != nil {
		return err
	}

	if err := svc.Start(); err != nil {
		return err
	}

	if err := svc.Enable(); err != nil {
		return err
	}

	if len(c.VPN) > 0 {
		if err := vpnSetupFN(); err != nil {
			return err
		}
	}

	return role.CreateSentinel()
}
