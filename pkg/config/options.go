package config

type Options struct {
	ScanDir          []string
	BootCMDLineFile  string
	MergeBootCMDLine bool
	NoLogs           bool
}

type Option func(o *Options) error

var NoLogs Option = func(o *Options) error {
	o.NoLogs = true
	return nil
}

func (o *Options) Apply(opts ...Option) error {
	for _, oo := range opts {
		if err := oo(o); err != nil {
			return err
		}
	}
	return nil
}

var MergeBootLine = func(o *Options) error {
	o.MergeBootCMDLine = true
	return nil
}

func WithBootCMDLineFile(s string) Option {
	return func(o *Options) error {
		o.BootCMDLineFile = s
		return nil
	}
}
func Directories(d ...string) Option {
	return func(o *Options) error {
		o.ScanDir = d
		return nil
	}
}
