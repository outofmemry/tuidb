package db

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/net/proxy"

	"webtuidb/internal/config"
)

// Tunnel is an open SSH connection; Dial routes driver connections through it.
type Tunnel struct {
	client *ssh.Client
	parent *Tunnel // the ProxyJump bastion this tunnel runs through
}

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

func (t *Tunnel) Close() error {
	err := t.client.Close()
	if t.parent != nil {
		t.parent.Close()
	}
	return err
}

// AskPassphrase is set by the UIs to prompt for the passphrase of an encrypted key; nil means
// encrypted keys fail with an error.
var AskPassphrase func(keyPath string) (string, error)

// SSHLog receives a debug trace of tunnel setup (the --ssh-log flag); nil is quiet.
var SSHLog io.Writer

func sshLog(format string, a ...any) {
	if w := SSHLog; w != nil {
		fmt.Fprintf(w, "ssh: "+format+"\n", a...)
	}
}

// DialFor returns the dial function for c: through its SSH tunnel, else through its SOCKS5 proxy,
// else nil (direct). closer releases the tunnel and is never nil.
func DialFor(ctx context.Context, c config.Connection) (DialFunc, func(), error) {
	if c.SSH != nil && c.SSH.Host != "" {
		t, err := OpenTunnel(ctx, c.SSH)
		if err != nil {
			return nil, func() {}, err
		}
		return t.Dial, func() { t.Close() }, nil
	}
	if c.SocksProxy == "" {
		return nil, func() {}, nil
	}
	p, err := proxy.SOCKS5("tcp", c.SocksProxy, nil, proxy.Direct)
	if err != nil {
		return nil, func() {}, fmt.Errorf("socks proxy %q: %w", c.SocksProxy, err)
	}
	cd, ok := p.(proxy.ContextDialer)
	if !ok {
		return nil, func() {}, errors.New("socks proxy: dialer is not context-aware")
	}
	sshLog("database dial through SOCKS5 %s", c.SocksProxy)
	return cd.DialContext, func() {}, nil
}

// sshConfigJump returns the ProxyJump of the first matching Host block in the ssh_config at file.
func sshConfigJump(file, host string) string {
	return sshConfigGet(file, host, "proxyjump")["proxyjump"]
}

// sshConfigGet returns, for each (lowercase) key, its value from the first matching Host block that sets it,
// as ssh does; keys no block sets are absent.
func sshConfigGet(file, host string, keys ...string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(file)
	if err != nil {
		return out
	}
	match := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		k, v, _ := strings.Cut(strings.Replace(line, "=", " ", 1), " ")
		v = strings.TrimSpace(v)
		k = strings.ToLower(k)
		if _, seen := out[k]; match && !seen && slices.Contains(keys, k) {
			out[k] = strings.Trim(v, `"`)
			continue
		}
		if k == "host" {
			match = false
			for _, pat := range strings.Fields(v) {
				if neg := strings.HasPrefix(pat, "!"); neg {
					if ok, _ := path.Match(pat[1:], host); ok {
						match = false
						break
					}
				} else if ok, _ := path.Match(pat, host); ok {
					match = true
				}
			}
		}
	}
	return out
}

// localUser is the account name ssh uses when neither the spec nor ssh_config names one.
func localUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

// splitJump parses "user@host:port"; user and port may be missing.
func splitJump(spec string) (user, host string, port int) {
	if u, rest, ok := strings.Cut(spec, "@"); ok {
		user, spec = u, rest
	}
	host = spec
	if h, p, err := net.SplitHostPort(spec); err == nil {
		host = h
		port, _ = strconv.Atoi(p)
	}
	return
}

func loadSigner(cfg *config.SSH) (ssh.Signer, error) {
	pem, err := os.ReadFile(expandHome(cfg.Key))
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	var pm *ssh.PassphraseMissingError
	if errors.As(err, &pm) {
		if AskPassphrase == nil {
			return nil, fmt.Errorf("ssh key %s is encrypted (add it to ssh-agent)", cfg.Key)
		}
		pass, aerr := AskPassphrase(cfg.Key)
		if aerr != nil {
			return nil, aerr
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
	}
	if err != nil {
		return nil, fmt.Errorf("ssh key %s: %w", cfg.Key, err)
	}
	return signer, nil
}

// OpenTunnel dials cfg. Auth order: key file (passphrase from AskPassphrase), password, ssh-agent.
// Host keys are checked against ~/.ssh/known_hosts; an unknown host fails with a hint instead of
// trusting silently. A ProxyJump (cfg.ProxyJump, else the host's ~/.ssh/config entry; "none" turns
// it off) is dialled first and the target is reached through it.
// A bastion hop is resolved through its own ssh_config block (HostName, User, Port, IdentityFile) and is
// offered that key or else the target's key (loaded once), and the agent, never the target's password.
func OpenTunnel(ctx context.Context, cfg *config.SSH) (*Tunnel, error) {
	return openTunnel(ctx, cfg, false, nil)
}

func openTunnel(ctx context.Context, cfg *config.SSH, isHop bool, signer ssh.Signer) (*Tunnel, error) {
	if cfg == nil || cfg.Host == "" {
		return nil, errors.New("ssh tunnel needs a host")
	}
	var auth []ssh.AuthMethod
	if cfg.Key != "" {
		if signer == nil { // one passphrase prompt for the whole chain
			var err error
			if signer, err = loadSigner(cfg); err != nil {
				return nil, err
			}
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if cfg.Password != "" && !isHop {
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
	jump := cfg.ProxyJump
	if jump == "" {
		jump = sshConfigJump(filepath.Join(h, ".ssh", "config"), cfg.Host)
	}
	var raw net.Conn
	var parent *Tunnel
	if jump != "" && jump != "none" {
		hops := strings.Split(jump, ",")
		spec, host, hport := splitJump(strings.TrimSpace(hops[len(hops)-1]))
		// The bastion is its own host: the spec wins, then its ssh_config block, then the local user; the
		// target's key is only offered when the block names no key of its own.
		o := sshConfigGet(filepath.Join(h, ".ssh", "config"), host, "hostname", "user", "port", "identityfile")
		hop := *cfg
		hop.Host, hop.ProxyJump, hop.Password = cmp.Or(o["hostname"], host), "none", ""
		hop.User = cmp.Or(spec, o["user"], localUser())
		if hop.Port = hport; hop.Port == 0 {
			hop.Port, _ = strconv.Atoi(o["port"])
		}
		if k := o["identityfile"]; k != "" {
			if _, err := os.Stat(expandHome(k)); err == nil {
				hop.Key, signer = k, nil // its own key, loaded (and asked for) in the hop's call
			}
		}
		if len(hops) > 1 {
			hop.ProxyJump = strings.Join(hops[:len(hops)-1], ",")
		}
		sshLog("%s via bastion %s", addr, jump)
		if parent, err = openTunnel(ctx, &hop, true, signer); err != nil {
			return nil, fmt.Errorf("proxy jump %s: %w", jump, err)
		}
		if raw, err = parent.Dial(ctx, "tcp", addr); err != nil {
			parent.Close()
			return nil, err
		}
	} else {
		sshLog("dial %s as %s", addr, cfg.User)
		d := net.Dialer{Timeout: 15 * time.Second}
		if raw, err = d.DialContext(ctx, "tcp", addr); err != nil {
			return nil, err
		}
	}
	// ssh.NewClientConn ignores ClientConfig.Timeout, and a hop's channel conn has no deadline: close the
	// conn when the handshake takes too long or ctx ends.
	timer := time.AfterFunc(20*time.Second, func() { raw.Close() })
	stopCtx := context.AfterFunc(ctx, func() { raw.Close() })
	cc, chans, reqs, err := ssh.NewClientConn(raw, addr, &ssh.ClientConfig{User: cfg.User, Auth: auth, HostKeyCallback: hostKey, Timeout: 15 * time.Second})
	timer.Stop()
	stopCtx()
	if err != nil {
		raw.Close()
		if parent != nil {
			parent.Close()
		}
		sshLog("handshake with %s failed: %v", addr, err)
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && len(ke.Want) == 0 {
			return nil, fmt.Errorf("ssh: %s is not in ~/.ssh/known_hosts; run `ssh %s@%s` once to trust it", cfg.Host, cfg.User, cfg.Host)
		}
		return nil, fmt.Errorf("ssh: %w", err)
	}
	sshLog("connected to %s", addr)
	return &Tunnel{client: ssh.NewClient(cc, chans, reqs), parent: parent}, nil
}
