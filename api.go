package telexec

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source api.go -package telexec -destination mock.go

import (
	"context"
	"io"
)

// Runner represents an execution environment capable of creating commands.
type Runner interface {
	// Command creates a new Command to execute the specified binary and arguments.
	Command(binary string, args ...string) Command
}

// Command represents a fluent command invocation builder.
type Command interface {
	// Context sets the context for cancellation and timeout.
	Context(ctx context.Context) Command

	// Stdin sets the reader for standard input.
	Stdin(r io.Reader) Command

	// Stdout sets the writer for standard output.
	Stdout(w io.Writer) Command

	// Stderr sets the writer for standard error.
	Stderr(w io.Writer) Command

	// Run starts the command and waits for it to complete.
	Run() error
}
