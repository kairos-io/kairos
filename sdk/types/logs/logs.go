package loggather

// LogsConfig represents the configuration for log collection.
type LogsConfig struct {
	Journal []string `yaml:"journal,omitempty"`
	Files   []string `yaml:"files,omitempty"`
}
