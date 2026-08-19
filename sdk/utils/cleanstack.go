package utils

import (
	"github.com/hashicorp/go-multierror"
)

type CleanJob func() error

// NewCleanStack returns a new stack.
// It's used to push jobs into it that need to be executed in order, like unmounting disks or removing dirs, and it
// will run those jobs in the order they were pushed into it to maintain order
// So you can create a dir, push its removal into the stack, mount something into that dir and push its unmounting into
// the stack and when cleanup is triggered it will first unmount and then remove the dir
// Usually its setup inside a function with a defer immediately so it auto cleans if you return from anywhere in the function
// That way you don't need to track on each return what needs to be cleaned and whatnot
// cleanup := utils.NewCleanStack()
// defer func() { err = cleanup.Cleanup(err) }()
func NewCleanStack() *CleanStack {
	return &CleanStack{}
}

// CleanStack is a basic LIFO stack that resizes as needed.
type CleanStack struct {
	jobs    []CleanJob
	current int
}

// Push adds a node to the stack
func (clean *CleanStack) Push(job CleanJob) {
	clean.jobs = append(clean.jobs[:clean.current], job)
	clean.current++
}

// Pop removes and returns a node from the stack in last to first order.
func (clean *CleanStack) Pop() CleanJob {
	if clean.current == 0 {
		return nil
	}
	clean.current--
	return clean.jobs[clean.current]
}

// Cleanup runs the whole cleanup stack. In case of error it runs all jobs
// and returns the first error occurrence.
func (clean *CleanStack) Cleanup(err error) error {
	var errs error
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	for clean.current > 0 {
		job := clean.Pop()
		err = job()
		if err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs
}
