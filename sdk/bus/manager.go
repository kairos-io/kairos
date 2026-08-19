package bus

import (
	"os"

	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	"github.com/mudler/go-pluggable"
)

const (
	DefaultProviderPrefix = "agent-provider"
	DefaultLogName        = "kairos-bus"
)

var DefaultProviderPaths = []string{"/system/providers", "/usr/local/system/providers"}

func NewBus(withEvents ...pluggable.EventType) *Bus {
	if len(withEvents) == 0 {
		withEvents = AllEvents
	}
	return &Bus{
		Manager: pluggable.NewManager(withEvents),
	}
}

type Bus struct {
	*pluggable.Manager
	registered     bool
	logger         *sdkLogger.KairosLogger // Fully override the logger
	logLevel       string                  // Log level for the logger, defaults to "info" unless BUS_DEBUG is set to "true". This only valid if logger is not set.
	logName        string                  // Name of the logger, defaults to "bus". This only valid if logger is not set.
	providerPrefix string                  // Prefix for provider plugins, defaults to "agent-provider". This is used to autoload providers.
	providerPaths  []string                // Paths to search for provider plugins, defaults to system and current working directory.
}

func (b *Bus) LoadProviders() *pluggable.Manager {
	return b.Autoload(b.providerPrefix, b.providerPaths...).Register()
}

func (b *Bus) Initialize(o ...Options) {
	if b.registered {
		return
	}

	for _, opt := range o {
		opt(b)
	}

	// If no provider prefix is set, use the default "agent-provider"
	if b.providerPrefix == "" {
		b.providerPrefix = DefaultProviderPrefix
	}

	// If no provider paths are set, use the default system paths and current working directory
	if b.providerPaths == nil {
		wd, _ := os.Getwd()
		b.providerPaths = append(DefaultProviderPaths, wd)
	}

	// If no logger is set, create a new one with the default log level and name
	if b.logger == nil {
		if b.logLevel == "" {
			b.logLevel = "info"
		}

		if os.Getenv("BUS_DEBUG") == "true" {
			b.logLevel = "debug"
		}
		if b.logName == "" {
			b.logName = DefaultLogName
		}
		l := sdkLogger.NewKairosLogger(b.logName, b.logLevel, false)
		b.logger = &l
	}

	b.LoadProviders()
	b.registered = true
	b.logger.Logger.Debug().Interface("bus", b).Msg("Bus initialized with options")
}

type Options func(d *Bus)

// WithLogger allows to set a custom logger for the bus. If set, it will override the default logger.
func WithLogger(logger *sdkLogger.KairosLogger) Options {
	return func(d *Bus) {
		d.logger = logger
	}
}

// WithLoggerLevel allows to set the log level for the bus logger. If set, it will override the default log level.
func WithLoggerLevel(level string) Options {
	return func(d *Bus) {
		d.logLevel = level
	}
}

// WithLoggerName allows to set the name of the logger for the bus. If set, it will override the default logger name.
func WithLoggerName(name string) Options {
	return func(d *Bus) {
		d.logName = name
	}
}

// WithProviderPrefix allows to set the prefix for provider plugins. If set, it will override the default prefix.
func WithProviderPrefix(prefix string) Options {
	return func(d *Bus) {
		d.providerPrefix = prefix
	}
}

// WithProviderPaths allows to set the paths to search for provider plugins. If set, it will override the default paths.
func WithProviderPaths(paths ...string) Options {
	return func(d *Bus) {
		d.providerPaths = paths
	}
}
