package telexec

import (
	"context"
	"io"
)

// command provides the shared implementation for fluent command builders.
type command struct {
	ctx          context.Context
	binary       string
	args         []string
	stdin        io.Reader
	stdoutWriter io.Writer
	stderrWriter io.Writer
	runFn        func(c *command) error
}

func (c *command) Context(ctx context.Context) Command {
	cp := *c
	cp.ctx = ctx
	return &cp
}

func (c *command) Stdin(r io.Reader) Command {
	cp := *c
	cp.stdin = r
	return &cp
}

func (c *command) Stdout(w io.Writer) Command {
	cp := *c
	cp.stdoutWriter = w
	return &cp
}

func (c *command) Stderr(w io.Writer) Command {
	cp := *c
	cp.stderrWriter = w
	return &cp
}

func (c *command) Run() error {
	return c.runFn(c)
}
