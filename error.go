package telexec

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Error represents a command execution failure that captures the stderr output and exit code.
type Error struct {
	Binary   string
	Args     []string
	Err      error
	Stderr   string
	ExitCode int
}

func (e *Error) Error() string {
	cause := strings.TrimSpace(e.Stderr)
	if cause != "" {
		return fmt.Sprintf("%s: %s", e.Err.Error(), cause)
	}
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

func newCommandError(binary string, args []string, err error, stderr string) error {
	if err == nil {
		return nil
	}
	exitCode := -1
	var execErr *exec.ExitError
	var sshErr *ssh.ExitError
	if errors.As(err, &execErr) {
		exitCode = execErr.ExitCode()
	} else if errors.As(err, &sshErr) {
		exitCode = sshErr.ExitStatus()
	}
	return &Error{
		Binary:   binary,
		Args:     args,
		Err:      err,
		Stderr:   strings.TrimSpace(stderr),
		ExitCode: exitCode,
	}
}
