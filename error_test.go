package telexec

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func Test_Error_Formatting(t *testing.T) {
	underlying := errors.New("exit status 1")

	tests := []struct {
		name       string
		err        *Error
		wantString string
	}{
		{
			name: "WithStderrCause",
			err: &Error{
				Binary:   "zfs",
				Args:     []string{"destroy", "pool/fs"},
				Err:      underlying,
				Stderr:   "cannot destroy 'pool/fs': dataset is busy\n",
				ExitCode: 1,
			},
			wantString: "exit status 1: cannot destroy 'pool/fs': dataset is busy",
		},
		{
			name: "WithoutStderrCause",
			err: &Error{
				Binary:   "zfs",
				Args:     []string{"destroy", "pool/fs"},
				Err:      underlying,
				Stderr:   "",
				ExitCode: 1,
			},
			wantString: "exit status 1",
		},
		{
			name: "WhitespaceOnlyStderr",
			err: &Error{
				Binary:   "zfs",
				Args:     []string{"destroy", "pool/fs"},
				Err:      underlying,
				Stderr:   "   \n\t  ",
				ExitCode: 1,
			},
			wantString: "exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantString {
				t.Errorf("Error() = %q, want %q", got, tt.wantString)
			}
			if !errors.Is(tt.err, underlying) {
				t.Errorf("errors.Is(tt.err, underlying) = false, want true")
			}
		})
	}
}

func Test_command_StderrCapture(t *testing.T) {
	tests := []struct {
		name         string
		execute      func(runner Runner) error
		wantExitCode int
		wantStderr   string
	}{
		{
			name: "Wait_StderrAndExitCode",
			execute: func(runner Runner) error {
				return runner.Command("sh", "-c", "echo 'dataset is busy' >&2; exit 2").Run()
			},
			wantExitCode: 2,
			wantStderr:   "dataset is busy",
		},
		{
			name: "Wait_StderrAndExitCode_WithStderrWriter",
			execute: func(runner Runner) error {
				var errBuf bytes.Buffer
				err := runner.Command("sh", "-c", "echo 'failed to list' >&2; exit 1").
					Stderr(&errBuf).
					Run()
				return err
			},
			wantExitCode: 1,
			wantStderr:   "failed to list",
		},
		{
			name: "Streaming_StderrAndExitCode",
			execute: func(runner Runner) error {
				pr, pw := io.Pipe()
				var data []byte
				var readErr error
				readDone := make(chan struct{})
				go func() {
					defer close(readDone)
					data, readErr = io.ReadAll(pr)
				}()

				runErr := runner.Command("sh", "-c", "echo 'part1'; echo 'reader error' >&2; exit 3").
					Stdout(pw).
					Run()
				_ = pw.Close()
				<-readDone
				if readErr != nil {
					return readErr
				}
				if string(data) != "part1\n" {
					t.Errorf("ReadAll data = %q, want 'part1\\n'", string(data))
				}
				return runErr
			},
			wantExitCode: 3,
			wantStderr:   "reader error",
		},
		{
			name: "Stdin_StderrAndExitCode",
			execute: func(runner Runner) error {
				return runner.Command("sh", "-c", "cat > /dev/null; echo 'stdin error' >&2; exit 4").
					Stdin(strings.NewReader("some stream")).
					Run()
			},
			wantExitCode: 4,
			wantStderr:   "stdin error",
		},
		{
			name: "CustomStderr_StderrAndExitCode",
			execute: func(runner Runner) error {
				var customErr bytes.Buffer
				err := runner.Command("sh", "-c", "echo 'custom stderr' >&2; exit 5").
					Stderr(&customErr).
					Run()
				if !strings.Contains(customErr.String(), "custom stderr") {
					t.Errorf("customErr = %q, want containing 'custom stderr'", customErr.String())
				}
				return err
			},
			wantExitCode: 5,
			wantStderr:   "custom stderr",
		},
		{
			name: "Wait_MultiLineStderr_CapturesLastLine",
			execute: func(runner Runner) error {
				return runner.Command("sh", "-c", "echo 'step 1: starting' >&2; echo 'step 2: processing' >&2; echo 'fatal: crashed' >&2; echo '' >&2; exit 6").Run()
			},
			wantExitCode: 6,
			wantStderr:   "fatal: crashed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewLocalRunner()
			err := tt.execute(runner)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			var cmdErr *Error
			if !errors.As(err, &cmdErr) {
				t.Fatalf("expected error to be *Error, got %T: %v", err, err)
			}
			if cmdErr.ExitCode != tt.wantExitCode {
				t.Errorf("ExitCode = %d, want %d", cmdErr.ExitCode, tt.wantExitCode)
			}
			if cmdErr.Stderr != tt.wantStderr {
				t.Errorf("Stderr = %q, want %q", cmdErr.Stderr, tt.wantStderr)
			}
			if !strings.Contains(err.Error(), tt.wantStderr) {
				t.Errorf("err.Error() = %q, should contain %q", err.Error(), tt.wantStderr)
			}

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Errorf("errors.As(*exec.ExitError) = false, want true")
			}
		})
	}
}
