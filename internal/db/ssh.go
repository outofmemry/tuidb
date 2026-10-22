package db

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"tuidb/internal/config"
)

// Tunnel is an open SSH connection; Dial routes driver connections through it.
type Tunnel struct{ client *ssh.Client }

// Dial opens a channel through the tunnel; the ssh client has no context-aware dial, so a
// cancelled ctx returns early and the late channel (if any) is closed when it arrives.
func (t *Tunnel) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := t.client.Dial(network, addr)
		ch <- res{c, err}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.c != nil {
				r.c.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

func (t *Tunnel) Close() error { return t.client.Close() }

// OpenTunnel dials cfg. Auth order: key file, password, ssh-agent. Host keys are checked against
// ~/.ssh/known_hosts; an unknown host fails with a hint instead of trusting silently.
// shortcut: no passphrase prompt for encrypted keys, load them into ssh-agent instead.
func OpenTunnel(ctx context.Context, cfg *config.SSH) (*Tunnel, error) {
	if cfg == nil || cfg.Host == "" {
		return nil, errors.New("ssh tunnel needs a host")
	}
	var auth []ssh.AuthMethod
	if cfg.Key != "" {
		path := cfg.Key
		if strings.HasPrefix(path, "~/") {
			h, _ := os.UserHomeDir()
			path = filepath.Join(h, path[2:])
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("ssh key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			return nil, fmt.Errorf("ssh key %s: %w (encrypted keys: add them to ssh-agent)", cfg.Key, err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" {
		auth = append(auth, ssh.Password(cfg.Password))
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if c, err := net.Dial("unix", sock); err == nil {
			defer c.Close() // only needed while authenticating
			auth = append(auth, ssh.PublicKeysCallback(agent.NewClient(c).Signers))
		}
	}
	if len(auth) == 0 {
		return nil, errors.New("ssh: no key, password or running ssh-agent to authenticate with")
	}
	h, _ := os.UserHomeDir()
	hostKey, err := knownhosts.New(filepath.Join(h, ".ssh", "known_hosts"))
	if err != nil {
		return nil, fmt.Errorf("ssh: cannot read ~/.ssh/known_hosts: %w", err)
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(port))
	d := net.Dialer{Timeout: 15 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// ssh.NewClientConn ignores ClientConfig.Timeout: bound the handshake with a socket deadline
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	raw.SetDeadline(deadline)
	defer raw.SetDeadline(time.Time{})
	cc, chans, reqs, err := ssh.NewClientConn(raw, addr, &ssh.ClientConfig{User: cfg.User, Auth: auth, HostKeyCallback: hostKey, Timeout: 15 * time.Second})
	if err != nil {
		raw.Close()
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && len(ke.Want) == 0 {
			return nil, fmt.Errorf("ssh: %s is not in ~/.ssh/known_hosts; run `ssh %s@%s` once to trust it", cfg.Host, cfg.User, cfg.Host)
		}
		return nil, fmt.Errorf("ssh: %w", err)
	}
	return &Tunnel{client: ssh.NewClient(cc, chans, reqs)}, nil
}
