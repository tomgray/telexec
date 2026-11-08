package telexec

import (
	"io"
	"sync"
)

const maxLineLength = 1000

// Logger defines a function that accepts single log lines without line terminators.
type Logger func(string)

// PrintFunc defines a variadic printing function, compatible with log.Print, log.Println, or fmt.Println.
type PrintFunc func(...any)

// SlogFunc defines a structured logging function, compatible with slog.Info, slog.Debug, slog.Warn, or slog.Error.
type SlogFunc func(string, ...any)

type logWriter struct {
	mu     sync.Mutex
	logger Logger
	buffer []byte
}

// NewLogWriter creates an io.WriteCloser that buffers written bytes by line
// and invokes logger for each complete line. Any trailing data without a newline
// is flushed when Close is called. If logger is nil, NewLogWriter returns nil.
func NewLogWriter(logger Logger) io.WriteCloser {
	if logger == nil {
		return nil
	}
	return &logWriter{
		logger: logger,
		buffer: make([]byte, 0, maxLineLength),
	}
}

// NewPrintLogWriter creates an io.WriteCloser that buffers written bytes by line
// and invokes the given PrintFunc (such as log.Print) for each complete line.
func NewPrintLogWriter(fn PrintFunc) io.WriteCloser {
	return NewLogWriter(func(line string) {
		fn(line)
	})
}

// NewSlogWriter creates an io.WriteCloser that buffers written bytes by line
// and invokes the given SlogFunc (such as slog.Info) for each complete line.
func NewSlogWriter(fn SlogFunc) io.WriteCloser {
	return NewLogWriter(func(line string) {
		fn(line)
	})
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, b := range p {
		if b == '\n' {
			w.flush()
		} else {
			w.buffer = append(w.buffer, b)
			if len(w.buffer) >= maxLineLength {
				w.flush()
			}
		}
	}
	return len(p), nil
}

func (w *logWriter) flush() {
	w.logger(string(w.buffer))
	w.buffer = w.buffer[:0]
}

func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buffer) > 0 {
		w.flush()
	}
	return nil
}
