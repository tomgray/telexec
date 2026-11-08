package telexec

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func Test_LocalRunner_Run(t *testing.T) {
	runner := NewLocalRunner()

	tests := []struct {
		name    string
		binary  string
		args    []string
		wantErr bool
	}{
		{
			name:    "Success",
			binary:  "echo",
			args:    []string{"hello"},
			wantErr: false,
		},
		{
			name:    "Failure",
			binary:  "false",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runner.Command(tt.binary, tt.args...).Run()
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_LocalRunner_Stdout_Stderr_Buffers(t *testing.T) {
	runner := NewLocalRunner()

	tests := []struct {
		name       string
		binary     string
		args       []string
		wantStdout string
		wantStderr string
		wantErr    bool
	}{
		{
			name:       "Echo_Success",
			binary:     "echo",
			args:       []string{"hello"},
			wantStdout: "hello\n",
			wantErr:    false,
		},
		{
			name:       "Combined_Stdout_Stderr",
			binary:     "sh",
			args:       []string{"-c", "echo 'out'; echo 'err' >&2"},
			wantStdout: "out\n",
			wantStderr: "err\n",
			wantErr:    false,
		},
		{
			name:    "Exit_Failure",
			binary:  "false",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := runner.Command(tt.binary, tt.args...).
				Stdout(&stdout).
				Stderr(&stderr).
				Run()
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if stdout.String() != tt.wantStdout {
					t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
				}
				if stderr.String() != tt.wantStderr {
					t.Errorf("stderr = %q, want %q", stderr.String(), tt.wantStderr)
				}
			}
		})
	}
}

func Test_LocalRunner_Streaming_Stdout(t *testing.T) {
	runner := NewLocalRunner()

	pr, pw := io.Pipe()

	var data []byte
	var readErr error
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		data, readErr = io.ReadAll(pr)
	}()

	err := runner.Command("echo", "streaming").
		Stdout(pw).
		Run()
	_ = pw.Close()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	<-readDone
	if readErr != nil {
		t.Fatalf("unexpected ReadAll error: %v", readErr)
	}
	if string(data) != "streaming\n" {
		t.Errorf("data = %q, want 'streaming\\n'", string(data))
	}
}

func Test_LocalRunner_Stdin(t *testing.T) {
	runner := NewLocalRunner()

	var stdout bytes.Buffer
	input := "piped standard input"
	err := runner.Command("cat").
		Stdin(strings.NewReader(input)).
		Stdout(&stdout).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if stdout.String() != input {
		t.Errorf("stdout = %q, want %q", stdout.String(), input)
	}
}

func Test_LocalRunner_Stdout_Stderr_Redirection(t *testing.T) {
	runner := NewLocalRunner()

	var outBuf, errBuf bytes.Buffer
	err := runner.Command("sh", "-c", "echo 'out-redirect'; echo 'err-redirect' >&2").
		Stdout(&outBuf).
		Stderr(&errBuf).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if strings.TrimSpace(outBuf.String()) != "out-redirect" {
		t.Errorf("outBuf = %q, want 'out-redirect'", outBuf.String())
	}
	if strings.TrimSpace(errBuf.String()) != "err-redirect" {
		t.Errorf("errBuf = %q, want 'err-redirect'", errBuf.String())
	}
}

func Test_LocalRunner_Context_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	runner := NewLocalRunner()
	err := runner.Command("sleep", "10").
		Context(ctx).
		Run()
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}
}

func Test_LocalRunner_Context_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	runner := NewLocalRunner()
	err := runner.Command("sleep", "5").
		Context(ctx).
		Run()
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func Test_LocalRunner_Command_Immutability(t *testing.T) {
	runner := NewLocalRunner()
	base := runner.Command("cat")

	var out1, out2 bytes.Buffer
	cmd1 := base.Stdin(strings.NewReader("input1")).Stdout(&out1)
	cmd2 := base.Stdin(strings.NewReader("input2")).Stdout(&out2)

	err1 := cmd1.Run()
	if err1 != nil {
		t.Fatalf("unexpected cmd1 Run error: %v", err1)
	}

	err2 := cmd2.Run()
	if err2 != nil {
		t.Fatalf("unexpected cmd2 Run error: %v", err2)
	}

	if out1.String() != "input1" {
		t.Errorf("cmd1 output = %q, want 'input1'", out1.String())
	}
	if out2.String() != "input2" {
		t.Errorf("cmd2 output = %q, want 'input2'", out2.String())
	}
}

func Test_LocalRunner_Unset_Streams(t *testing.T) {
	runner := NewLocalRunner()

	// When neither Stdout nor Stderr is set, command executes normally and output is discarded
	err := runner.Command("echo", "discarded output").Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}
}

type trackingCloserWriter struct {
	bytes.Buffer
	closed bool
}

func (w *trackingCloserWriter) Close() error {
	w.closed = true
	return nil
}

func Test_LocalRunner_UserWriterNotClosed(t *testing.T) {
	runner := NewLocalRunner()

	stdoutTracker := &trackingCloserWriter{}
	stderrTracker := &trackingCloserWriter{}

	err := runner.Command("sh", "-c", "echo 'out'; echo 'err' >&2").
		Stdout(stdoutTracker).
		Stderr(stderrTracker).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	// User-provided writers should NOT be closed by telexec
	if stdoutTracker.closed {
		t.Errorf("expected user stdout writer NOT to be closed by command")
	}
	if stderrTracker.closed {
		t.Errorf("expected user stderr writer NOT to be closed by command")
	}
	if strings.TrimSpace(stdoutTracker.String()) != "out" {
		t.Errorf("stdout = %q, want 'out'", stdoutTracker.String())
	}
	if strings.TrimSpace(stderrTracker.String()) != "err" {
		t.Errorf("stderr = %q, want 'err'", stderrTracker.String())
	}
}
