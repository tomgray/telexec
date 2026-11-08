package telexec

import (
	"context"
	"io"
	"os/exec"
)

type localRunner struct{}

// NewLocalRunner creates a Runner for local command execution.
func NewLocalRunner() Runner {
	return &localRunner{}
}

func (r *localRunner) Command(binary string, args ...string) Command {
	return &command{
		binary: binary,
		args:   args,
		runFn:  runLocal,
	}
}

func runLocal(c *command) error {
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	execCmd := exec.CommandContext(ctx, c.binary, c.args...)
	if c.stdin != nil {
		execCmd.Stdin = c.stdin
	}

	// Configure stdout
	if c.stdoutWriter != nil {
		execCmd.Stdout = c.stdoutWriter
	}

	// Configure stderr & capture
	capture := newCaptureWriter()
	if c.stderrWriter != nil {
		execCmd.Stderr = io.MultiWriter(c.stderrWriter, capture)
	} else {
		execCmd.Stderr = capture
	}

	err := execCmd.Run()
	return newCommandError(c.binary, c.args, err, capture.String())
}
