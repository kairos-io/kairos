package stages

import (
	"github.com/kairos-io/kairos/kairos-init/pkg/config"
	"github.com/kairos-io/kairos/kairos-init/pkg/system"
	"github.com/kairos-io/kairos/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/sdk/types/logger"
	"github.com/mudler/yip/pkg/console"
	"github.com/mudler/yip/pkg/executor"
	"github.com/mudler/yip/pkg/schema"
	"github.com/twpayne/go-vfs/v5"
)

// RunAllStages Runs all the stages in the correct order
func RunAllStages(logger logger.KairosLogger) (schema.YipConfig, error) {
	fullYipConfig := schema.YipConfig{Stages: map[string][]schema.Stage{}}
	installStage, err := RunInstallStage(logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to run the install stage: %s", err)
		return installStage, err
	}

	// Add all stages to the full yip config
	for stageName, stages := range installStage.Stages {
		fullYipConfig.Stages[stageName] = append(fullYipConfig.Stages[stageName], stages...)
	}

	initStage, err := RunInitStage(logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to run the init stage: %s", err)
		return fullYipConfig, err
	}

	// Add all stages to the full yip config
	for stageName, stages := range initStage.Stages {
		fullYipConfig.Stages[stageName] = append(fullYipConfig.Stages[stageName], stages...)
	}

	return fullYipConfig, nil
}

// RunInstallStage Runs the install stage
// This is good if we are doing the init in layers as this will allow us to run the install stage and cache that then run
// the init stage later so we can cache the install stage which is usually the longest
func RunInstallStage(logger logger.KairosLogger) (schema.YipConfig, error) {
	if config.ContainsSkipStep(values.InstallStage) {
		logger.Logger.Warn().Msg("Skipping install stage as per configuration")
		return schema.YipConfig{Stages: map[string][]schema.Stage{}}, nil
	}
	logger.Info("Running stage install")
	sis := system.DetectSystem(logger)
	initExecutor := executor.NewExecutor(executor.WithLogger(logger))
	yipConsole := console.NewStandardConsole(console.WithLogger(logger))

	data := schema.YipConfig{Stages: map[string][]schema.Stage{}}
	// Run things before we install packages

	// Initialize the "before-install" stage; it will be populated with model-specific repo tweaks and extensions
	data.Stages["before-install"] = []schema.Stage{}

	// On Rpi3 and Rpi4 we need to enable the non-free repository for Debian to get the firmware
	if config.DefaultConfig.Model == values.Rpi3.String() || config.DefaultConfig.Model == values.Rpi4.String() {
		data.Stages["before-install"] = append(data.Stages["before-install"], []schema.Stage{
			{
				Name:     "Enable non-free repository",
				OnlyIfOs: "Debian.*",
				Commands: []string{
					"sed -i 's/^Components: main.*$/& non-free-firmware/' /etc/apt/sources.list.d/debian.sources",
				},
			},
		}...)
	}
	// Add extensions from disk
	data.Stages["before-install"] = append(data.Stages["before-install"], GetStageExtensions("before-install", logger)...)

	// Add packages install
	installStage, err := GetInstallStage(sis, logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the install stage: %s", err)
		return data, err
	}
	data.Stages["install"] = installStage

	// Get kernel stage
	kernelStage, err := GetInstallKernelStage(sis, logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the kernel stage: %s", err)
		return data, err
	}
	data.Stages["install"] = append(data.Stages["install"], kernelStage...)
	// Add the branding files
	data.Stages["install"] = append(data.Stages["install"], GetInstallBrandingStage(sis, logger)...)
	// Add the bootargs file
	data.Stages["install"] = append(data.Stages["install"], GetInstallGrubBootArgsStage(sis, logger)...)
	// Add the miscellaneous files
	data.Stages["install"] = append(data.Stages["install"], GetKairosMiscellaneousFilesStage(sis, logger)...)
	// Add extensions from disk
	data.Stages["install"] = append(data.Stages["install"], GetStageExtensions("install", logger)...)
	// Run things after we install packages and framework
	data.Stages["after-install"] = []schema.Stage{}
	// Add extensions from disk
	data.Stages["after-install"] = append(data.Stages["after-install"], GetStageExtensions("after-install", logger)...)

	for _, st := range []string{"before-install", "install", "after-install"} {
		err = initExecutor.Run(st, vfs.OSFS, yipConsole, data.ToString())
		if err != nil {
			logger.Logger.Error().Msgf("Failed to run the %s stage: %s", st, err)
			return data, err
		}
	}

	// Copy the configs in the system
	err = GetInstallOemCloudConfigs(logger)
	if err != nil {
		return schema.YipConfig{}, err
	}

	// Bring kairos binaries
	err = GetInstallKairosBinaries(sis, logger)
	if err != nil {
		return schema.YipConfig{}, err
	}

	// Bring provider binaries
	err = GetInstallProviderBinaries(sis, logger)
	if err != nil {
		return schema.YipConfig{}, err
	}

	// Trigger the build install event for providers
	err = ProviderBuildInstallEvent(sis, logger)
	if err != nil {
		return schema.YipConfig{}, err
	}

	return data, nil
}

// RunInitStage Runs the init stage
// This is good if we are doing the init in layers as this will allow us to run the install stage and cache that then run
// the init stage later so we can cache the install stage which is usually the longest
func RunInitStage(logger logger.KairosLogger) (schema.YipConfig, error) {
	if config.ContainsSkipStep(values.InitStage) {
		logger.Logger.Warn().Msg("Skipping init stage as per configuration")
		return schema.YipConfig{Stages: map[string][]schema.Stage{}}, nil
	}
	logger.Info("Running stage init")
	sis := system.DetectSystem(logger)
	initExecutor := executor.NewExecutor(executor.WithLogger(logger))
	yipConsole := console.NewStandardConsole(console.WithLogger(logger))

	data := schema.YipConfig{Stages: map[string][]schema.Stage{}}

	// Run things before we init the system
	data.Stages["before-init"] = []schema.Stage{}

	// Add extensions from disk
	data.Stages["before-init"] = append(data.Stages["before-init"], GetStageExtensions("before-init", logger)...)

	data.Stages["init"] = []schema.Stage{}
	data.Stages["init"] = append(data.Stages["init"], GetKairosReleaseStage(sis, logger)...)
	kernelStage, err := GetKernelStage(sis, logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the kernel stage: %s", err)
		return data, err
	}
	data.Stages["init"] = append(data.Stages["init"], kernelStage...)

	// Add initrd files before rebuilding initrd
	initrdFilesStage, err := GetKairosInitramfsFilesStage(sis, logger)
	if err != nil {
		return data, err
	}
	data.Stages["init"] = append(data.Stages["init"], initrdFilesStage...)

	// Rebuild the initrd
	initrdStage, err := GetInitrdStage(sis, logger)
	if err != nil {
		logger.Logger.Error().Msgf("Failed to get the initrd stage: %s", err)
		return data, err
	}
	data.Stages["init"] = append(data.Stages["init"], initrdStage...)
	data.Stages["init"] = append(data.Stages["init"], GetServicesStage(sis, logger)...)
	data.Stages["init"] = append(data.Stages["init"], GetSshHardeningStage(sis, logger)...)
	data.Stages["init"] = append(data.Stages["init"], GetWorkaroundsStage(sis, logger)...)
	data.Stages["init"] = append(data.Stages["init"], GetCleanupStage(sis, logger)...)

	// Add extensions from disk
	data.Stages["init"] = append(data.Stages["init"], GetStageExtensions("init", logger)...)

	// Run things after we init the system
	data.Stages["after-init"] = []schema.Stage{}

	// Add extensions from disk
	data.Stages["after-init"] = append(data.Stages["after-init"], GetStageExtensions("after-init", logger)...)

	for _, st := range []string{"before-init", "init", "after-init"} {
		err = initExecutor.Run(st, vfs.OSFS, yipConsole, data.ToString())
		if err != nil {
			logger.Logger.Error().Msgf("Failed to run the %s stage: %s", st, err)
			return data, err
		}
	}

	return data, nil
}
