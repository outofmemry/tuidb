package db

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"webtuidb/internal/config"
)

func TestSSHConfigJump(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host other\n  ProxyJump nope\nHost *.internal !skip.internal\n  ProxyJump=me@bastion:2222\nHost *\n  ProxyJump late\n"), 0o600)
	for host, want := range map[string]string{"db.internal": "me@bastion:2222", "skip.internal": "late", "other": "nope", "x": "late"} {
		if got := sshConfigJump(f, host); got != want {
			t.Errorf("%s: %q want %q", host, got, want)
		}
	}
	if u, h, p := splitJump("me@bastion:2222"); u != "me" || h != "bastion" || p != 2222 {
		t.Errorf("%q %q %d", u, h, p)
	}
	if u, h, p := splitJump("bastion"); u != "" || h != "bastion" || p != 0 {
		t.Errorf("%q %q %d", u, h, p)
	}
}

func TestSSHConfigGetFirstMatchWins(t *testing.T) {
	f := filepath.Join(t.TempDir(), "config")
	os.WriteFile(f, []byte("Host bastion\n  User ubuntu\n  Port 2222\n  IdentityFile \"~/.ssh/b\"\nHost *\n  User other\n  HostName x\n"), 0o600)
	got := sshConfigGet(f, "bastion", "user", "port", "identityfile", "hostname", "proxyjump")
	want := map[string]string{"user": "ubuntu", "port": "2222", "identityfile": "~/.ssh/b", "hostname": "x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v want %v", got, want)
	}
}

func TestHandshakeDeadlineAndHopNeverGetsTargetPassword(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), nil, 0o600)
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // accepts and says nothing: no SSH banner
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := OpenTunnel(ctx, &config.SSH{Host: host, Port: p, User: "u", Password: "x"}); err == nil || time.Since(start) > 5*time.Second {
		t.Errorf("silent server: err=%v after %v", err, time.Since(start))
	}
	// the hop has no key or agent: the target's password must not be offered to it
	_, err = OpenTunnel(context.Background(), &config.SSH{Host: "target", User: "u", Password: "x", ProxyJump: ln.Addr().String()})
	if err == nil || !strings.Contains(err.Error(), "no key") {
		t.Errorf("hop err = %v", err)
	}
}
