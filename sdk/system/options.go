package system

import (
	"github.com/hashicorp/go-multierror"
)

type Changeset []func() error

func (c *Changeset) Add(f func() error) {
	*c = append(*c, f)
}

type Option func(c *Changeset) error

func Apply(opts ...Option) error {

	c := &Changeset{}
	for _, o := range opts {
		if err := o(c); err != nil {
			return err
		}
	}

	var err error
	for _, f := range *c {
		err = multierror.Append(f())
	}

	return err
}
