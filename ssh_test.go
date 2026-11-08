package telexec

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// startTestSSHServer starts an in-process RFC 4254 SSH server on 127.0.0.1 that executes
// commands via sh -c, exactly like a real remote OpenSSH daemon.
func startTestSSHServer(t *testing.T) (*ssh.Client, func()) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		NoClientAuth: true,
	}
	serverConfig.AddHostKey(signer)

	addr, serverCleanup := startTestSSHServerWithConfig(t, serverConfig)

	clientConfig := &ssh.ClientConfig{
		User:            "testuser",
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
		Timeout:         5 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, clientConfig)
	if err != nil {
		serverCleanup()
		t.Fatalf("failed to dial test ssh server: %v", err)
	}

	cleanup := func() {
		_ = client.Close()
		serverCleanup()
	}

	return client, cleanup
}

func startTestSSHServerWithConfig(t *testing.T, serverConfig *ssh.ServerConfig) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}

	go func() {
		for {
			nConn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleServerConn(nConn, serverConfig)
		}
	}()

	cleanup := func() {
		_ = listener.Close()
	}

	return listener.Addr().String(), cleanup
}

// testAgentSocket creates a short Unix domain socket path for ssh-agent testing.
// We avoid t.TempDir() because its paths include the full test name and can exceed
// the OS limit for sockaddr_un (104 bytes on macOS/Darwin, 108 bytes on Linux)
// in deeply nested build environments such as Nix.
func testAgentSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "a.sock")
}

func handleServerConn(nConn net.Conn, config *ssh.ServerConfig) {
	sConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		_ = nConn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		ch, requests, err := newChan.Accept()
		if err != nil {
			continue
		}
		go handleSessionRequests(ch, requests)
	}
	_ = sConn.Close()
}

func handleSessionRequests(ch ssh.Channel, requests <-chan *ssh.Request) {
	defer ch.Close()

	for req := range requests {
		switch req.Type {
		case "exec":
			if len(req.Payload) < 4 {
				_ = req.Reply(false, nil)
				return
			}
			cmdLen := binary.BigEndian.Uint32(req.Payload[:4])
			cmdStr := string(req.Payload[4 : 4+cmdLen])
			_ = req.Reply(true, nil)

			cmd := exec.Command("sh", "-c", cmdStr)
			cmd.Stdin = ch
			cmd.Stdout = ch
			cmd.Stderr = ch.Stderr()

			err := cmd.Run()
			exitCode := uint32(0)
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exitCode = uint32(exitErr.ExitCode())
				} else {
					exitCode = 255
				}
			}

			exitStatusPayload := make([]byte, 4)
			binary.BigEndian.PutUint32(exitStatusPayload, exitCode)
			_, _ = ch.SendRequest("exit-status", false, exitStatusPayload)
			return

		default:
			_ = req.Reply(false, nil)
		}
	}
}

func Test_SSHRunner_Execution(t *testing.T) {
	client, cleanup := startTestSSHServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		execute      func(runner Runner) error
		wantStderr   string
		wantExitCode int
	}{
		{
			name: "Run_Success",
			execute: func(runner Runner) error {
				return runner.Command("echo", "hello-run").Run()
			},
		},
		{
			name: "Run_Failure_StderrAndExitCode",
			execute: func(runner Runner) error {
				return runner.Command("sh", "-c", "echo 'remote error' >&2; exit 2").Run()
			},
			wantStderr:   "remote error",
			wantExitCode: 2,
		},
		{
			name: "Stdout_Success",
			execute: func(runner Runner) error {
				var stdout bytes.Buffer
				err := runner.Command("echo", "hello-stdout").Stdout(&stdout).Run()
				if err != nil {
					return err
				}
				if strings.TrimSpace(stdout.String()) != "hello-stdout" {
					return fmt.Errorf("unexpected output: %q", stdout.String())
				}
				return nil
			},
		},
		{
			name: "ExitFailure_StderrCaptured",
			execute: func(runner Runner) error {
				return runner.Command("sh", "-c", "echo 'stdout failed' >&2; exit 5").Run()
			},
			wantStderr:   "stdout failed",
			wantExitCode: 5,
		},
		{
			name: "Streaming_Stdout",
			execute: func(runner Runner) error {
				pr, pw := io.Pipe()
				var data []byte
				var readErr error
				readDone := make(chan struct{})
				go func() {
					defer close(readDone)
					data, readErr = io.ReadAll(pr)
				}()

				runErr := runner.Command("echo", "streamed-data").Stdout(pw).Run()
				_ = pw.Close()
				<-readDone
				if readErr != nil {
					return readErr
				}
				if strings.TrimSpace(string(data)) != "streamed-data" {
					return fmt.Errorf("unexpected streamed output: %q", string(data))
				}
				return runErr
			},
		},
		{
			name: "Streaming_FailureExitCode",
			execute: func(runner Runner) error {
				pr, pw := io.Pipe()
				readDone := make(chan struct{})
				go func() {
					defer close(readDone)
					_, _ = io.ReadAll(pr)
				}()

				runErr := runner.Command("sh", "-c", "echo 'output'; echo 'stream failed' >&2; exit 7").Stdout(pw).Run()
				_ = pw.Close()
				<-readDone
				return runErr
			},
			wantStderr:   "stream failed",
			wantExitCode: 7,
		},
		{
			name: "Stdin_Success",
			execute: func(runner Runner) error {
				var stdout bytes.Buffer
				err := runner.Command("cat").
					Stdin(strings.NewReader("piped input")).
					Stdout(&stdout).
					Run()
				if err != nil {
					return err
				}
				if stdout.String() != "piped input" {
					return fmt.Errorf("unexpected stdin echoed output: %q", stdout.String())
				}
				return nil
			},
		},
		{
			name: "Stdin_Failure",
			execute: func(runner Runner) error {
				return runner.Command("sh", "-c", "cat > /dev/null; echo 'stdin failed' >&2; exit 9").
					Stdin(strings.NewReader("input")).
					Run()
			},
			wantStderr:   "stdin failed",
			wantExitCode: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewSSHRunnerWithClient(client)
			err := tt.execute(runner)

			if tt.wantStderr != "" || tt.wantExitCode != 0 {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var cmdErr *Error
				if !errors.As(err, &cmdErr) {
					t.Fatalf("expected *Error, got %T: %v", err, err)
				}
				if cmdErr.ExitCode != tt.wantExitCode {
					t.Errorf("ExitCode = %d, want %d", cmdErr.ExitCode, tt.wantExitCode)
				}
				if cmdErr.Stderr != tt.wantStderr {
					t.Errorf("Stderr = %q, want %q", cmdErr.Stderr, tt.wantStderr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func Test_SSHRunner_ConcurrentSessions(t *testing.T) {
	client, cleanup := startTestSSHServer(t)
	defer cleanup()

	runner := NewSSHRunnerWithClient(client)
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var stdout bytes.Buffer
			err := runner.Command("echo", fmt.Sprintf("concurrent-%d", idx)).
				Stdout(&stdout).
				Run()
			if err != nil {
				errCh <- fmt.Errorf("session %d run error: %w", idx, err)
				return
			}
			if strings.TrimSpace(stdout.String()) != fmt.Sprintf("concurrent-%d", idx) {
				errCh <- fmt.Errorf("session %d bad output: %q", idx, stdout.String())
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent execution error: %v", err)
	}
}

func Test_SSHRunner_Quoting(t *testing.T) {
	client, cleanup := startTestSSHServer(t)
	defer cleanup()

	runner := NewSSHRunnerWithClient(client)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "SpacesInArguments",
			args: []string{"printf", "%s", "hello world"},
			want: "hello world",
		},
		{
			name: "QuotesAndApostrophes",
			args: []string{"printf", "%s", `it's "quoted"`},
			want: `it's "quoted"`,
		},
		{
			name: "GlobCharacters",
			args: []string{"printf", "%s", "pool/*@snap$"},
			want: "pool/*@snap$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			err := runner.Command(tt.args[0], tt.args[1:]...).
				Stdout(&stdout).
				Run()
			if err != nil {
				t.Fatalf("unexpected Run error: %v", err)
			}
			if stdout.String() != tt.want {
				t.Errorf("output = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func Test_SSHRunner_Close(t *testing.T) {
	client, cleanup := startTestSSHServer(t)
	defer cleanup()

	runner := NewSSHRunnerWithClient(client)

	// verify works before Close
	err := runner.Command("true").Run()
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}

	// cast to io.Closer and close
	closer, ok := runner.(io.Closer)
	if !ok {
		t.Fatalf("expected sshRunner to implement io.Closer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("unexpected Close() error: %v", err)
	}

	// verify returns error after Close
	err = runner.Command("true").Run()
	if err == nil {
		t.Fatalf("expected error after Close(), got nil")
	}
}

func Test_SSHRunner_ContextCancellation(t *testing.T) {
	client, cleanup := startTestSSHServer(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := NewSSHRunnerWithClient(client)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := runner.Command("sleep", "5").
		Context(ctx).
		Run()
	if err == nil {
		t.Fatalf("expected command to be cancelled, but succeeded")
	}
}

func Test_NewSSHRunner_EagerConnection(t *testing.T) {
	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeDir)

	// Closed port should fail eagerly
	_, err := NewSSHRunner("127.0.0.1:59999")
	if err == nil {
		t.Fatalf("expected connection error for unreachable host, got nil")
	}
}

func Test_parseRemoteSpec(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatalf("failed to get current user: %v", err)
	}
	defaultUser := u.Username

	tests := []struct {
		spec     string
		wantUser string
		wantHost string
		wantPort string
	}{
		{"example.com", defaultUser, "example.com", "22"},
		{"alice@example.com", "alice", "example.com", "22"},
		{"example.com:2222", defaultUser, "example.com", "2222"},
		{"bob@example.com:2222", "bob", "example.com", "2222"},
		{"[::1]:2222", defaultUser, "::1", "2222"},
		{"admin@[::1]:2222", "admin", "::1", "2222"},
		{"::1", defaultUser, "::1", "22"},
		{"[::1]", defaultUser, "::1", "22"},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			gotUser, gotHost, gotPort := parseRemoteSpec(tt.spec)
			if gotUser != tt.wantUser || gotHost != tt.wantHost || gotPort != tt.wantPort {
				t.Errorf("parseRemoteSpec(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.spec, gotUser, gotHost, gotPort, tt.wantUser, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func Test_loadSignersFromDir(t *testing.T) {
	t.Run("NonExistentDir", func(t *testing.T) {
		signers := loadSignersFromDir("/non/existent/path")
		if len(signers) != 0 {
			t.Errorf("expected 0 signers, got %d", len(signers))
		}
	})

	t.Run("EmptyDir", func(t *testing.T) {
		dir := t.TempDir()
		signers := loadSignersFromDir(dir)
		if len(signers) != 0 {
			t.Errorf("expected 0 signers, got %d", len(signers))
		}
	})

	t.Run("InvalidKeys", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), []byte("not a valid key"), 0600); err != nil {
			t.Fatal(err)
		}
		signers := loadSignersFromDir(dir)
		if len(signers) != 0 {
			t.Errorf("expected 0 signers, got %d", len(signers))
		}
	})

	t.Run("ValidKeys", func(t *testing.T) {
		dir := t.TempDir()

		// Generate ed25519 key
		_, privEd, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		blockEd, err := ssh.MarshalPrivateKey(privEd, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "id_ed25519"), pem.EncodeToMemory(blockEd), 0600); err != nil {
			t.Fatal(err)
		}

		// Generate ECDSA key
		privEcdsa, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		blockEcdsa, err := ssh.MarshalPrivateKey(privEcdsa, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "id_ecdsa"), pem.EncodeToMemory(blockEcdsa), 0600); err != nil {
			t.Fatal(err)
		}

		signers := loadSignersFromDir(dir)
		if len(signers) != 2 {
			t.Fatalf("expected 2 signers, got %d", len(signers))
		}
	})
}

func Test_defaultHostKeyCallback(t *testing.T) {
	t.Run("ErrorWhenNoKnownHosts", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cb, err := defaultHostKeyCallback()
		if err == nil {
			t.Fatal("expected error when known_hosts is missing, got nil")
		}
		if cb != nil {
			t.Fatal("expected nil callback when known_hosts is missing")
		}
	})

	t.Run("SuccessWhenKnownHostsPresent", func(t *testing.T) {
		dir := t.TempDir()
		sshDir := filepath.Join(dir, ".ssh")
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			t.Fatal(err)
		}
		knownHostsFile := filepath.Join(sshDir, "known_hosts")
		if err := os.WriteFile(knownHostsFile, []byte(""), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", dir)
		cb, err := defaultHostKeyCallback()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cb == nil {
			t.Fatal("expected non-nil callback")
		}
	})
}

func Test_defaultAuthMethods(t *testing.T) {
	t.Run("NoAgent_NoKeys", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "")
		t.Setenv("HOME", t.TempDir())
		authMethods, closer := defaultAuthMethods()
		if closer != nil {
			_ = closer.Close()
		}
		if len(authMethods) != 0 {
			t.Errorf("expected 0 auth methods, got %d", len(authMethods))
		}
	})

	t.Run("WithDiskKeys", func(t *testing.T) {
		t.Setenv("SSH_AUTH_SOCK", "")
		dir := t.TempDir()
		sshDir := filepath.Join(dir, ".ssh")
		if err := os.MkdirAll(sshDir, 0700); err != nil {
			t.Fatal(err)
		}
		_, privEd, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		blockEd, err := ssh.MarshalPrivateKey(privEd, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), pem.EncodeToMemory(blockEd), 0600); err != nil {
			t.Fatal(err)
		}

		t.Setenv("HOME", dir)
		authMethods, closer := defaultAuthMethods()
		if closer != nil {
			_ = closer.Close()
		}
		if len(authMethods) != 1 {
			t.Fatalf("expected 1 auth method, got %d", len(authMethods))
		}
	})

	t.Run("WithAgent", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		keyring := agent.NewKeyring()
		_, privEd, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if err := keyring.Add(agent.AddedKey{PrivateKey: privEd}); err != nil {
			t.Fatal(err)
		}

		sockPath := testAgentSocket(t)
		l, err := net.Listen("unix", sockPath)
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()

		go func() {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go agent.ServeAgent(keyring, conn)
			}
		}()

		t.Setenv("SSH_AUTH_SOCK", sockPath)
		authMethods, closer := defaultAuthMethods()
		if closer == nil {
			t.Fatal("expected non-nil closer for agent")
		}
		defer closer.Close()

		if len(authMethods) != 1 {
			t.Fatalf("expected 1 auth method, got %d", len(authMethods))
		}
	})
}

func Test_NewSSHRunner_Integration_AgentAuth(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverSigner, err := ssh.NewSignerFromKey(serverPriv)
	if err != nil {
		t.Fatal(err)
	}

	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == "agentuser" && bytes.Equal(key.Marshal(), clientSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized key for user %s", conn.User())
		},
	}
	serverConfig.AddHostKey(serverSigner)

	addr, serverCleanup := startTestSSHServerWithConfig(t, serverConfig)
	defer serverCleanup()

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: clientPriv}); err != nil {
		t.Fatal(err)
	}

	sockPath := testAgentSocket(t)
	agentListener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer agentListener.Close()

	go func() {
		for {
			conn, err := agentListener.Accept()
			if err != nil {
				return
			}
			go agent.ServeAgent(keyring, conn)
		}
	}()

	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	knownHostsLine := knownhosts.Line([]string{knownhosts.Normalize(addr)}, serverSigner.PublicKey()) + "\n"
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(knownHostsLine), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSH_AUTH_SOCK", sockPath)
	t.Setenv("HOME", homeDir)

	runner, err := NewSSHRunner(fmt.Sprintf("agentuser@%s", addr))
	if err != nil {
		t.Fatalf("failed to dial with agent auth: %v", err)
	}
	defer runner.(io.Closer).Close()

	var stdout bytes.Buffer
	err = runner.Command("echo", "agent-dial-ok").Stdout(&stdout).Run()
	if err != nil {
		t.Fatalf("failed to run command: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "agent-dial-ok" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func Test_NewSSHRunner_Integration_DiskKeyAuth(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverSigner, err := ssh.NewSignerFromKey(serverPriv)
	if err != nil {
		t.Fatal(err)
	}

	_, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == "diskuser" && bytes.Equal(key.Marshal(), clientSigner.PublicKey().Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized key for user %s", conn.User())
		},
	}
	serverConfig.AddHostKey(serverSigner)

	addr, serverCleanup := startTestSSHServerWithConfig(t, serverConfig)
	defer serverCleanup()

	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(clientPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	knownHostsLine := knownhosts.Line([]string{knownhosts.Normalize(addr)}, serverSigner.PublicKey()) + "\n"
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(knownHostsLine), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", homeDir)

	runner, err := NewSSHRunner(fmt.Sprintf("diskuser@%s", addr))
	if err != nil {
		t.Fatalf("failed to dial with disk key auth: %v", err)
	}
	defer runner.(io.Closer).Close()

	var stdout bytes.Buffer
	err = runner.Command("echo", "disk-dial-ok").Stdout(&stdout).Run()
	if err != nil {
		t.Fatalf("failed to run command: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != "disk-dial-ok" {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func Test_NewSSHRunner_Integration_AuthFailure(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverSigner, err := ssh.NewSignerFromKey(serverPriv)
	if err != nil {
		t.Fatal(err)
	}

	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, fmt.Errorf("no keys accepted")
		},
	}
	serverConfig.AddHostKey(serverSigner)

	addr, serverCleanup := startTestSSHServerWithConfig(t, serverConfig)
	defer serverCleanup()

	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	knownHostsLine := knownhosts.Line([]string{knownhosts.Normalize(addr)}, serverSigner.PublicKey()) + "\n"
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(knownHostsLine), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", homeDir)

	_, err = NewSSHRunner(fmt.Sprintf("nobody@%s", addr))
	if err == nil {
		t.Fatalf("expected authentication failure error, got nil")
	}
}

func Test_NewSSHRunner_Integration_HostKeyVerificationFailure(t *testing.T) {
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverSigner, err := ssh.NewSignerFromKey(serverPriv)
	if err != nil {
		t.Fatal(err)
	}

	serverConfig := &ssh.ServerConfig{
		NoClientAuth: true,
	}
	serverConfig.AddHostKey(serverSigner)

	addr, serverCleanup := startTestSSHServerWithConfig(t, serverConfig)
	defer serverCleanup()

	// Empty known_hosts file - server key is not trusted
	homeDir := t.TempDir()
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "known_hosts"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOME", homeDir)

	_, err = NewSSHRunner(fmt.Sprintf("user@%s", addr))
	if err == nil {
		t.Fatalf("expected host key verification error for untrusted host, got nil")
	}
}

func Test_NewSSHRunner_MissingKnownHosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := NewSSHRunner("example.com:22")
	if err == nil {
		t.Fatalf("expected error when known_hosts is missing, got nil")
	}
	if !strings.Contains(err.Error(), "host key verification") {
		t.Fatalf("expected error to mention host key verification, got: %v", err)
	}
}

func Test_testAgentSocket(t *testing.T) {
	sockPath := testAgentSocket(t)

	// Unix socket path length must be strictly less than 104 bytes (macOS/Darwin sockaddr_un limit).
	if len(sockPath) >= 104 {
		t.Fatalf("socket path length %d exceeds 103 bytes: %s", len(sockPath), sockPath)
	}

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket %s: %v", sockPath, err)
	}
	defer l.Close()
}
