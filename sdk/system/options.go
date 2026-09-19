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

	// Collect every failure rather than keeping only the last one, and hand
	// back a plain nil when nothing failed. multierror.Append always returns
	// a non-nil *multierror.Error, so assigning it straight to an error
	// interface reports failure even for an empty list.
	var errs *multierror.Error
	for _, f := range *c {
		errs = multierror.Append(errs, f())
	}

	return errs.ErrorOrNil()
}
