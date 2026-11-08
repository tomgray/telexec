package telexec

import (
	"bytes"
	"errors"
	"io"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func Test_Command_Stdout_LogWriter(t *testing.T) {
	var logs []string
	logger := func(line string) {
		logs = append(logs, line)
	}

	lw := NewLogWriter(logger)
	defer lw.Close()

	runner := NewLocalRunner()
	if err := runner.Command("sh", "-c", "echo 'line 1'; echo 'line 2'").
		Stdout(lw).
		Run(); err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	wantLogs := []string{"line 1", "line 2"}
	if len(logs) != len(wantLogs) {
		t.Fatalf("got logs %v, want %v", logs, wantLogs)
	}
	for i, want := range wantLogs {
		if logs[i] != want {
			t.Errorf("logs[%d] = %q, want %q", i, logs[i], want)
		}
	}
}

func Test_Command_Stderr_LogWriter(t *testing.T) {
	var logs []string
	logger := func(line string) {
		logs = append(logs, line)
	}

	lw := NewLogWriter(logger)
	defer lw.Close()

	runner := NewLocalRunner()
	if err := runner.Command("sh", "-c", "echo 'err 1' >&2; echo 'err 2' >&2").
		Stderr(lw).
		Run(); err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	wantLogs := []string{"err 1", "err 2"}
	if len(logs) != len(wantLogs) {
		t.Fatalf("got logs %v, want %v", logs, wantLogs)
	}
	for i, want := range wantLogs {
		if logs[i] != want {
			t.Errorf("logs[%d] = %q, want %q", i, logs[i], want)
		}
	}
}

func Test_Command_LogWriter_FlushedOnClose(t *testing.T) {
	var logs []string
	logger := func(line string) {
		logs = append(logs, line)
	}

	lw := NewLogWriter(logger)
	runner := NewLocalRunner()
	if err := runner.Command("printf", "unterminated line").
		Stdout(lw).
		Run(); err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if len(logs) != 0 {
		t.Fatalf("expected logs to be empty before Close, got %v", logs)
	}

	if err := lw.Close(); err != nil {
		t.Fatalf("unexpected Close error: %v", err)
	}

	wantLogs := []string{"unterminated line"}
	if len(logs) != len(wantLogs) || logs[0] != wantLogs[0] {
		t.Fatalf("got logs %v, want %v", logs, wantLogs)
	}
}

func Test_Command_LogWriter_ErrorCapture(t *testing.T) {
	var logs []string
	logger := func(line string) {
		logs = append(logs, line)
	}

	lw := NewLogWriter(logger)
	defer lw.Close()

	runner := NewLocalRunner()
	err := runner.Command("sh", "-c", "echo 'fatal failure' >&2; exit 3").
		Stderr(lw).
		Run()
	if err == nil {
		t.Fatalf("expected error from non-zero exit, got nil")
	}

	var cmdErr *Error
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if cmdErr.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", cmdErr.ExitCode)
	}
	if cmdErr.Stderr != "fatal failure" {
		t.Errorf("Stderr = %q, want 'fatal failure'", cmdErr.Stderr)
	}
	if len(logs) != 1 || logs[0] != "fatal failure" {
		t.Errorf("logs = %v, want ['fatal failure']", logs)
	}
}

func Test_Command_SharedLogWriter(t *testing.T) {
	var logs []string
	logger := func(line string) {
		logs = append(logs, line)
	}

	lw := NewLogWriter(logger)
	defer lw.Close()

	runner := NewLocalRunner()
	err := runner.Command("sh", "-c", "echo 'out line'; echo 'err line' >&2").
		Stdout(lw).
		Stderr(lw).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %v", logs)
	}
}

func Test_Command_LogWriter_WithMultiWriter(t *testing.T) {
	var logs []string
	logger := func(line string) {
		logs = append(logs, line)
	}

	var buf bytes.Buffer
	lw := NewLogWriter(logger)
	defer lw.Close()

	runner := NewLocalRunner()
	err := runner.Command("echo", "tee-test").
		Stdout(io.MultiWriter(lw, &buf)).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	if strings.TrimSpace(buf.String()) != "tee-test" {
		t.Errorf("buf = %q, want 'tee-test'", buf.String())
	}
	if len(logs) != 1 || logs[0] != "tee-test" {
		t.Errorf("logs = %v, want ['tee-test']", logs)
	}
}

func Test_NewLogWriter_NilLogger(t *testing.T) {
	w := NewLogWriter(nil)
	if w != nil {
		t.Errorf("expected nil writer for nil logger, got %v", w)
	}
}

func Test_LogWriter_LineBuffering(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		close    bool
		expected []string
	}{
		{
			name:     "singleLineWithNewline",
			inputs:   []string{"hello world\n"},
			close:    true,
			expected: []string{"hello world"},
		},
		{
			name:     "singleLineWithoutNewlineFlushedOnClose",
			inputs:   []string{"hello world"},
			close:    true,
			expected: []string{"hello world"},
		},
		{
			name:     "singleLineChunkedWrites",
			inputs:   []string{"hello ", "brave ", "new ", "world\n"},
			close:    true,
			expected: []string{"hello brave new world"},
		},
		{
			name:     "multipleLinesSingleWrite",
			inputs:   []string{"line 1\nline 2\nline 3\n"},
			close:    true,
			expected: []string{"line 1", "line 2", "line 3"},
		},
		{
			name:     "emptyLines",
			inputs:   []string{"\n\n\n"},
			close:    true,
			expected: []string{"", "", ""},
		},
		{
			name:     "closeWithNoWrites",
			inputs:   nil,
			close:    true,
			expected: nil,
		},
		{
			name:     "exceedsMaxLineLength",
			inputs:   []string{strings.Repeat("a", maxLineLength+10) + "\n"},
			close:    true,
			expected: []string{strings.Repeat("a", maxLineLength), strings.Repeat("a", 10)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var lines []string
			logger := func(line string) {
				lines = append(lines, line)
			}

			w := NewLogWriter(logger)
			if w == nil {
				t.Fatalf("expected non-nil writer")
			}

			for _, in := range tc.inputs {
				n, err := w.Write([]byte(in))
				if err != nil {
					t.Fatalf("Write error: %v", err)
				}
				if n != len(in) {
					t.Fatalf("Write wrote %d bytes, want %d", n, len(in))
				}
			}

			if tc.close {
				if err := w.Close(); err != nil {
					t.Fatalf("Close error: %v", err)
				}
			}

			if len(lines) != len(tc.expected) {
				t.Fatalf("got lines %v (len %d), want %v (len %d)", lines, len(lines), tc.expected, len(tc.expected))
			}
			for i := range lines {
				if lines[i] != tc.expected[i] {
					t.Errorf("line[%d] = %q, want %q", i, lines[i], tc.expected[i])
				}
			}
		})
	}
}

func Test_NewPrintLogWriter_DirectLogPrint(t *testing.T) {
	var buf bytes.Buffer
	stdLog := log.New(&buf, "", 0)

	lw := NewPrintLogWriter(stdLog.Print)
	defer lw.Close()

	runner := NewLocalRunner()
	err := runner.Command("sh", "-c", "echo 'std line 1'; echo 'std line 2'").
		Stdout(lw).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	want := "std line 1\nstd line 2\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func Test_NewSlogWriter_DirectSlogInfo(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	slogger := slog.New(handler)

	lw := NewSlogWriter(slogger.Info)
	defer lw.Close()

	runner := NewLocalRunner()
	err := runner.Command("sh", "-c", "echo 'slog line 1'; echo 'slog line 2'").
		Stdout(lw).
		Run()
	if err != nil {
		t.Fatalf("unexpected Run error: %v", err)
	}

	want := "level=INFO msg=\"slog line 1\"\nlevel=INFO msg=\"slog line 2\"\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func Test_NewPrintLogWriter_FlushesOnClose(t *testing.T) {
	var printed []string
	printer := func(v ...any) {
		for _, arg := range v {
			if s, ok := arg.(string); ok {
				printed = append(printed, s)
			}
		}
	}

	lw := NewPrintLogWriter(printer)
	if _, err := lw.Write([]byte("unterminated print line")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	if len(printed) != 0 {
		t.Fatalf("expected printed to be empty before Close, got %v", printed)
	}

	if err := lw.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	want := []string{"unterminated print line"}
	if len(printed) != len(want) || printed[0] != want[0] {
		t.Fatalf("got %v, want %v", printed, want)
	}
}

func Test_NewSlogWriter_FlushesOnClose(t *testing.T) {
	var logged []string
	slogFn := func(msg string, args ...any) {
		logged = append(logged, msg)
	}

	lw := NewSlogWriter(slogFn)
	if _, err := lw.Write([]byte("unterminated slog line")); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}

	if len(logged) != 0 {
		t.Fatalf("expected logged to be empty before Close, got %v", logged)
	}

	if err := lw.Close(); err != nil {
		t.Fatalf("unexpected close error: %v", err)
	}

	want := []string{"unterminated slog line"}
	if len(logged) != len(want) || logged[0] != want[0] {
		t.Fatalf("got %v, want %v", logged, want)
	}
}

func Test_LogWriter_TypeCompatibility(t *testing.T) {
	// Verify package-level log.Print matches PrintFunc without closure
	pw := NewPrintLogWriter(log.Print)
	if pw == nil {
		t.Fatal("expected non-nil writer")
	}
	_ = pw.Close()

	// Verify package-level slog.Info matches SlogFunc without closure
	sw := NewSlogWriter(slog.Info)
	if sw == nil {
		t.Fatal("expected non-nil writer")
	}
	_ = sw.Close()
}
