# telexec

`telexec` is a lightweight Go library providing a unified, fluent command execution engine for local and remote
environments with some convenient helpers for common use cases.

## Why telexec?

Go applications that manage systems or orchestrate infrastructure may need to run commands on local and remote hosts.
`telexec` provides a single API that can be used to execute commands on the local host or a remote host. A mock
implementation is also provided for ease of testing.

Features:

- **Location Transparency**: A single [`Runner`](api.go) interface abstracts whether commands execute locally (via
  `os/exec`) or remotely over a persistent SSH connection (via `golang.org/x/crypto/ssh`) without requiring any
  additional services running on the remote host. A persistent SSH connection allows sending multiple commands to the
  remote host without the overhead of requiring a new SSH connection to be set up and destroyed for each command.
- **Fluent Builder API**: Configure contexts, standard input, and output streams cleanly through immutable [
  `Command`](api.go) builders.
- **Actionable Error Reporting**: Automatically captures the last non-empty line of `stderr` and the process exit code
  into a structured [`*telexec.Error`](error.go), even when `stderr` is simultaneously streamed elsewhere.
- **Line-Buffered Logging**: Process output streams can be easily sent to a logging backend via built-in
  `io.WriteCloser` adapters.
- **First-Class Testability**: Ships with pre-generated [`gomock`](https://github.com/uber-go/mock) mocks (`MockRunner`,
  `MockCommand`) so callers can unit test command orchestration without spawning processes or opening network
  connections.

## Installation

```bash
go get github.com/tomgray/telexec
```

## Quick Start

### Local Command Execution

Use `NewLocalRunner` to execute commands on the local machine:

```go
package main

import (
	"bytes"
	"fmt"
	"log"

	"github.com/tomgray/telexec"
)

func main() {
	runner := telexec.NewLocalRunner()

	var out bytes.Buffer
	err := runner.Command("uname", "-s", "-r").
		Stdout(&out).
		Run()
	if err != nil {
		log.Fatalf("command failed: %v", err)
	}

	fmt.Printf("Kernel: %s", out.String())
}
```

### Remote Command Execution over SSH

Use `NewSSHRunner` to connect to a remote host and execute commands over a persistent SSH connection:

```go
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"

	"github.com/tomgray/telexec"
)

func main() {
	// Format: [user@]host[:port]
	// Defaults to current user and port 22.
	runner, err := telexec.NewSSHRunner("admin@server.example.com:22")
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	if closer, ok := runner.(io.Closer); ok {
		defer closer.Close()
	}

	var out bytes.Buffer
	if err := runner.Command("uptime").Stdout(&out).Run(); err != nil {
		log.Fatalf("remote command failed: %v", err)
	}

	fmt.Println(out.String())
}

```

`NewSSHRunner` automatically configures authentication and host key verification:

- **Authentication**: Uses `SSH_AUTH_SOCK` (SSH agent) if available, and loads standard private keys (`id_ed25519`,
  `id_ecdsa`, `id_rsa`, `id_dsa`) from `~/.ssh/`.
- **Host Key Verification**: Enforces strict host key checking against `~/.ssh/known_hosts`.
- **Custom Client**: If you need custom `ssh.ClientConfig` settings, dial your own `*ssh.Client` and wrap it with
  `telexec.NewSSHRunnerWithClient(client)`.

## User Guide

### Stream Redirection (`Stdin`, `Stdout`, `Stderr`)

`Command` provides methods that make it convenient to connect the command's input and output streams to `bytes.Buffer`
instances to provide input or capture output easily.

```go
input := strings.NewReader("hello world\n")
var stdout, stderr bytes.Buffer

err := runner.Command("tr", "a-z", "A-Z").
Stdin(input).
Stdout(&stdout).
Stderr(&stderr).
Run()
```

### Contexts, Timeouts, and Cancellation

Pass a `context.Context` to enforce timeouts or cancel in-flight local processes and remote SSH sessions:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

err := runner.Command("sleep", "60").
Context(ctx).
Run()
```

### Line-Buffered Logging

Command output arrives in arbitrary byte chunks. `telexec` provides `io.WriteCloser` helpers that buffer output and emit
complete lines to your logger:

- **`NewSlogWriter`**: Adapts `slog.Info`, `slog.Warn`, `slog.Error`, or `slog.Debug`.
- **`NewPrintLogWriter`**: Adapts `log.Print`, `log.Println`, or `fmt.Println`.
- **`NewLogWriter`**: Accepts a `func(line string)` callback.

```go
stdoutLog := telexec.NewSlogWriter(slog.Info)
defer stdoutLog.Close()

stderrLog := telexec.NewPrintLogWriter(log.Println)
defer stderrLog.Close()

err := runner.Command("sh", "-c", "echo 'starting task'; echo 'warning' >&2").
Stdout(stdoutLog).
Stderr(stderrLog).
Run()
```

### Error Handling

When a command exits with a non-zero status, `Run()` returns a `*telexec.Error` that includes the exit code and the last
non-empty line written to `stderr`:

```go
err := runner.Command("zfs", "destroy", "tank/nonexistent").Run()
if err != nil {
var cmdErr *telexec.Error
if errors.As(err, &cmdErr) {
fmt.Printf("Binary: %s\n", cmdErr.Binary)
fmt.Printf("Exit Code: %d\n", cmdErr.ExitCode)
fmt.Printf("Stderr Summary: %s\n", cmdErr.Stderr)
}
// err.Error() formats as "<underlying error>: <last line of stderr>"
}
```

### Unit Testing with Mocks

`telexec` includes `MockRunner` and `MockCommand` (generated via `go.uber.org/mock/gomock`) so components that accept
`telexec.Runner` can be tested without executing external binaries:

```go
func TestMyService(t *testing.T) {
ctrl := gomock.NewController(t)
mockRunner := telexec.NewMockRunner(ctrl)
mockCmd := telexec.NewMockCommand(ctrl)

mockRunner.EXPECT().
Command("systemctl", "restart", "nginx").
Return(mockCmd)
mockCmd.EXPECT().
Run().
Return(nil)

// Pass mockRunner into the code under test
err := RestartNginx(mockRunner)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
}
```

## Development

This project uses [Nix Flakes](https://nixos.wiki/wiki/Flakes) for reproducible development environments and builds:

```bash
# Enter development shell (Go 1.27, mockgen, nixpkgs-fmt)
nix develop

# Run unit and integration tests
go test -v ./...

# Build package and run flake checks
nix flake check

# Regenerate gomock mocks
nix run .#mock
```

## License

Licensed under the [Apache License, Version 2.0](LICENSE).
