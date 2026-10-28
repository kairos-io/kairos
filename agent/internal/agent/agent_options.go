package agent

// Options yields the options for the running agent.
type Options struct {
	APIAddress string
	Dir        []string
	Restart    bool
}

// Apply applies option to the options struct.
func (o *Options) Apply(opts ...Option) error {
	for _, oo := range opts {
		if err := oo(o); err != nil {
			return err
		}
	}
	return nil
}

// Option is a generic option for the Agent.
type Option func(o *Options) error

// RestartAgent makes the agent restart on error.
var RestartAgent Option = func(o *Options) error {
	o.Restart = true
	return nil
}

// WithAPI sets the API address used to talk to EdgeVPN and co-ordinate node bootstrapping.
func WithAPI(address string) Option {
	return func(o *Options) error {
		o.APIAddress = address
		return nil
	}
}

// WithDirectory sets the Agent config directories.
func WithDirectory(dirs ...string) Option {
	return func(o *Options) error {
		o.Dir = dirs
		return nil
	}
}
