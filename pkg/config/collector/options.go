package collector

import "fmt"

type Options struct {
	ScanDir          []string
	BootCMDLineFile  string
	MergeBootCMDLine bool
	NoLogs           bool
	StrictValidation bool
}

type Option func(o *Options) error

var NoLogs Option = func(o *Options) error {
	o.NoLogs = true
	return nil
}

// SoftErr prints a warning if err is no nil and NoLogs is not true.
// It's use to wrap the same handling happening in multiple places.
//
// TODO: Switch to a standard logging library (e.g. verbose, silent mode etc).
func (o *Options) SoftErr(message string, err error) {
	if !o.NoLogs && err != nil {
		fmt.Printf("WARNING: %s, %s\n", message, err.Error())
	}
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

func StrictValidation(v bool) Option {
	return func(o *Options) error {
		o.StrictValidation = v
		return nil
	}
}

func Directories(d ...string) Option {
	return func(o *Options) error {
		o.ScanDir = d
		return nil
	}
}
