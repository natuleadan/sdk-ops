package ssh

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/term"
)

type Client struct {
	host     string
	port     int
	user     string
	key      string
	usePW    bool
	insecure bool
}

type Option func(*Client)

func WithPort(port int) Option {
	return func(c *Client) { c.port = port }
}

func WithKey(key string) Option {
	return func(c *Client) { c.key = key }
}

func WithInsecure() Option {
	return func(c *Client) { c.insecure = true }
}

func New(host, user string, opts ...Option) *Client {
	c := &Client{
		host: host,
		port: 22,
		user: user,
		key:  os.ExpandEnv("$HOME/.ssh/id_ed25519"),
	}
	for _, o := range opts {
		o(c)
	}
	if _, err := os.Stat(c.key); os.IsNotExist(err) {
		c.key = os.ExpandEnv("$HOME/.ssh/id_rsa")
	}
	if _, err := os.Stat(c.key); os.IsNotExist(err) {
		c.usePW = true
	}
	return c
}

func hostKeyCallback() (ssh.HostKeyCallback, error) {
	isStrict := os.Getenv("SDK_OPS_SSH_STRICT_HOST_KEY") == "true" || os.Getenv("SDK_OPS_SSH_STRICT_HOST_KEY") == "1"
	khPath := filepath.Join(os.Getenv("HOME"), ".ssh", "known_hosts")
	if cb, err := knownhosts.New(khPath); err == nil {
		if isStrict {
			return cb, nil
		}
	}
	if isStrict {
		return nil, fmt.Errorf("known_hosts not available (set SDK_OPS_SSH_STRICT_HOST_KEY=false or create ~/.ssh/known_hosts)")
	}
	return ssh.InsecureIgnoreHostKey(), nil
}

func (c *Client) authMethods() ([]ssh.AuthMethod, error) {
	// Try SSH agent first
	authSock := os.Getenv("SSH_AUTH_SOCK")
	if authSock != "" && !strings.Contains(authSock, "..") {
		sockPath := filepath.Clean(authSock)
		if sockPath != "." && strings.HasPrefix(sockPath, "/") && !strings.Contains(sockPath, "..") {
			fi, err := os.Stat(sockPath)
			if err == nil && fi.Mode()&os.ModeSocket != 0 {
				if conn, err := net.Dial("unix", sockPath); err == nil {
					agentClient := agent.NewClient(conn)
					signers, err := agentClient.Signers()
					if err == nil && len(signers) > 0 {
						return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
					}
					conn.Close()
				}
			}
		}
	}

	if c.usePW {
		fmt.Printf("🔑 Password for %s@%s: ", c.user, c.host)
		pass, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return nil, fmt.Errorf("read password: %w", err)
		}
		return []ssh.AuthMethod{ssh.Password(string(pass))}, nil
	}

	key, err := os.ReadFile(c.key)
	if err != nil {
		return nil, fmt.Errorf("read key %s: %w", c.key, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse key %s: %w", c.key, err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

func (c *Client) Connect() (*ssh.Client, error) {
	auth, err := c.authMethods()
	if err != nil {
		return nil, err
	}

	hostCallback, err := hostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("host key callback: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            c.user,
		Auth:            auth,
		HostKeyCallback: hostCallback,
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(c.host, fmt.Sprintf("%d", c.port))
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

func Run(client *ssh.Client, cmd string) (string, string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		return string(out), "", fmt.Errorf("run %q: %w\noutput: %s", cmd, err, string(out))
	}
	return string(out), "", nil
}

func RunStream(client *ssh.Client, cmd string) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	outReader, err := sess.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	errReader, err := sess.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		_, cerr := io.Copy(os.Stdout, outReader)
		done <- cerr
	}()

	go func() {
		io.Copy(os.Stderr, errReader)
	}()

	if err := sess.Wait(); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return <-done
}

func RunPTY(client *ssh.Client, cmd string) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sess.Close()

	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr
	sess.Stdin = os.Stdin

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", 80, 40, modes); err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	return sess.Run(cmd)
}
