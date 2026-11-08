package telexec

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

func Test_CaptureWriter_LineCapture(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		expected string
	}{
		{
			name:     "SingleLine_WithoutNewline",
			inputs:   []string{"error without newline"},
			expected: "error without newline",
		},
		{
			name:     "SingleLine_WithNewline",
			inputs:   []string{"error with newline\n"},
			expected: "error with newline",
		},
		{
			name:     "MultiLine",
			inputs:   []string{"step 1: initialize\nstep 2: processing\nstep 3: fatal failure\n"},
			expected: "step 3: fatal failure",
		},
		{
			name:     "TrailingBlankLines",
			inputs:   []string{"info: starting\nerror: something broke\n\n\n"},
			expected: "error: something broke",
		},
		{
			name:     "ChunkedWrites",
			inputs:   []string{"first ", "line\n", "sec", "ond ", "line\n", "thi", "rd ", "line"},
			expected: "third line",
		},
		{
			name:     "LongLineWithoutTruncation",
			inputs:   []string{strings.Repeat("x", 500) + "\n"},
			expected: strings.Repeat("x", 500),
		},
		{
			name:     "MultipleLongLines",
			inputs:   []string{strings.Repeat("a", 300) + "\n", "final short error\n"},
			expected: "final short error",
		},
		{
			name:     "RollingBufferDiscardsOldest",
			inputs:   []string{strings.Repeat("old data\n", 1000), "most recent error line\n"},
			expected: "most recent error line",
		},
		{
			name:     "EmptyOrWhitespaceOnly",
			inputs:   []string{"\n\n\n"},
			expected: "",
		},
		{
			name:     "CRLF",
			inputs:   []string{"line 1\r\nline 2\r\n"},
			expected: "line 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cw := newCaptureWriter()
			for _, in := range tt.inputs {
				n, err := cw.Write([]byte(in))
				if err != nil {
					t.Fatalf("unexpected Write error: %v", err)
				}
				if n != len(in) {
					t.Fatalf("Write returned n = %d, want %d", n, len(in))
				}
			}
			if got := cw.String(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

func Test_CaptureWriter_PassthroughWithMultiWriter(t *testing.T) {
	var downstream bytes.Buffer
	cw := newCaptureWriter()
	mw := io.MultiWriter(&downstream, cw)

	input := "line 1\nline 2\nline 3\n"
	n, err := mw.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != len(input) {
		t.Fatalf("n = %d, want %d", n, len(input))
	}

	// Downstream buffer receives the full, unaltered stream
	if downstream.String() != input {
		t.Errorf("downstream = %q, want %q", downstream.String(), input)
	}
	// cw.String() only retains the last line
	if got := cw.String(); got != "line 3" {
		t.Errorf("cw.String() = %q, want 'line 3'", got)
	}
}

func Test_CaptureWriter_ConcurrentWrites(t *testing.T) {
	cw := newCaptureWriter()

	var wg sync.WaitGroup
	const workers = 10
	const iterations = 50

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, _ = cw.Write([]byte("concurrent message\n"))
			}
		}()
	}
	wg.Wait()

	if len(cw.String()) == 0 {
		t.Errorf("expected non-empty captured output")
	}
	if len(cw.String()) > maxCaptureBuffer {
		t.Errorf("captured output %d exceeded maxCaptureBuffer %d", len(cw.String()), maxCaptureBuffer)
	}
}
