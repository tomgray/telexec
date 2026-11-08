package telexec

import (
	"bytes"
	"sync"
)

const maxCaptureBuffer = 1024 // 1KB rolling buffer

type captureWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{}
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.buf.Write(p)
	if c.buf.Len() > maxCaptureBuffer {
		c.buf.Next(c.buf.Len() - maxCaptureBuffer)
	}
	c.mu.Unlock()

	return len(p), nil
}

func (c *captureWriter) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	data := bytes.TrimRight(c.buf.Bytes(), "\r\n")
	if len(data) == 0 {
		return ""
	}

	if idx := bytes.LastIndexByte(data, '\n'); idx != -1 {
		data = data[idx+1:]
	}
	return string(bytes.TrimRight(data, "\r"))
}
