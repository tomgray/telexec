package telexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type sshRunner struct {
	client *ssh.Client
}

// NewSSHRunner connects to the specified remote host and returns a Runner for remote execution.
func NewSSHRunner(remoteSpec string) (Runner, error) {
	client, err := dialSSH(remoteSpec)
	if err != nil {
		return nil, err
	}
	return NewSSHRunnerWithClient(client), nil
}

// NewSSHRunnerWithClient wraps an existing *ssh.Client in a Runner.
func NewSSHRunnerWithClient(client *ssh.Client) Runner {
	return &sshRunner{client: client}
}

// Close closes the underlying persistent SSH client connection.
func (r *sshRunner) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

func (r *sshRunner) Command(binary string, args ...string) Command {
	return &command{
		binary: binary,
		args:   args,
		runFn:  r.runSSH,
	}
}

func (r *sshRunner) runSSH(c *command) error {
	if r.client == nil {
		return errors.New("ssh client is not connected")
	}

	session, err := r.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	if c.ctx != nil {
		cancelWatcher := context.AfterFunc(c.ctx, func() {
			_ = session.Close()
		})
		defer cancelWatcher()
	}

	if c.stdin != nil {
		session.Stdin = c.stdin
	}

	// Configure stdout
	if c.stdoutWriter != nil {
		session.Stdout = c.stdoutWriter
	}

	// Configure stderr & capture
	capture := newCaptureWriter()
	if c.stderrWriter != nil {
		session.Stderr = io.MultiWriter(c.stderrWriter, capture)
	} else {
		session.Stderr = capture
	}

	cmdStr := formatCommand(c.binary, c.args)
	err = session.Run(cmdStr)
	return newCommandError(c.binary, c.args, err, capture.String())
}

func formatCommand(binary string, args []string) string {
	parts := make([]string, 1+len(args))
	parts[0] = quoteArg(binary)
	for i, a := range args {
		parts[i+1] = quoteArg(a)
	}
	return strings.Join(parts, " ")
}

func quoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func dialSSH(remoteSpec string) (*ssh.Client, error) {
	username, host, port := parseRemoteSpec(remoteSpec)

	authMethods, closer := defaultAuthMethods()
	if closer != nil {
		defer closer.Close()
	}

	hostKeyCallback, err := defaultHostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("failed to configure host key verification: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(host, port), cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s over ssh: %w", remoteSpec, err)
	}
	return client, nil
}

func defaultAuthMethods() ([]ssh.AuthMethod, io.Closer) {
	var authMethods []ssh.AuthMethod
	var agentConn io.Closer

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			agentConn = conn
			agentClient := agent.NewClient(conn)
			authMethods = append(authMethods, ssh.PublicKeysCallback(agentClient.Signers))
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		if signers := loadSignersFromDir(filepath.Join(home, ".ssh")); len(signers) > 0 {
			authMethods = append(authMethods, ssh.PublicKeys(signers...))
		}
	}

	return authMethods, agentConn
}

func loadSignersFromDir(dir string) []ssh.Signer {
	var signers []ssh.Signer
	for _, keyFile := range []string{"id_ed25519", "id_ecdsa", "id_rsa", "id_dsa"} {
		p := filepath.Join(dir, keyFile)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if key, err := ssh.ParsePrivateKey(data); err == nil {
			signers = append(signers, key)
		}
	}
	return signers
}

func defaultHostKeyCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	knownHostsFile := filepath.Join(home, ".ssh", "known_hosts")
	cb, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load known_hosts from %s: %w", knownHostsFile, err)
	}
	return cb, nil
}

func parseRemoteSpec(spec string) (username, host, port string) {
	port = "22"
	target := spec
	if at := strings.Index(target, "@"); at != -1 {
		username = target[:at]
		target = target[at+1:]
	} else if u, err := user.Current(); err == nil {
		username = u.Username
	}
	if h, p, err := net.SplitHostPort(target); err == nil {
		host = h
		port = p
	} else {
		host = strings.Trim(target, "[]")
	}
	return username, host, port
}
