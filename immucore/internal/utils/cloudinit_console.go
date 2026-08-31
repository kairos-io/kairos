package utils

import (
	"fmt"
	"os/exec"

	"github.com/hashicorp/go-multierror"
)

// ImmucoreConsole is the console for yip. As we have to hijack the Run method to be able to run under UKI
// To add the paths, we need to create our own console.
type ImmucoreConsole struct {
}

func (s ImmucoreConsole) Run(cmd string, opts ...func(cmd *exec.Cmd)) (string, error) {
	c := PrepareCommandWithPath(cmd)
	for _, o := range opts {
		o(c)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("failed to run %s: %v", cmd, err)
	}

	return string(out), err
}

func (s ImmucoreConsole) Start(cmd *exec.Cmd, opts ...func(cmd *exec.Cmd)) error {
	for _, o := range opts {
		o(cmd)
	}
	return cmd.Run()
}

func (s ImmucoreConsole) RunTemplate(st []string, template string) error {
	var errs error

	for _, svc := range st {
		out, err := s.Run(fmt.Sprintf(template, svc))
		if err != nil {
			KLog.Logger.Debug().Str("output", out).Msg("Run template")
			errs = multierror.Append(errs, err)
			continue
		}
	}
	return errs
}
