package config

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/crypto/scrypt"
)

const portableMagic = "WTDB1\n"

func portableKey(pass string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(pass), salt, 1<<15, 8, 1, 32)
}

// ExportConnections writes cs as TOML, sealed with AES-256-GCM under a scrypt key of passphrase
// (plain TOML when passphrase is ""). Without withPasswords the stored passwords are blanked;
// password_cmd is a command, not a secret, and stays.
func ExportConnections(w io.Writer, cs []Connection, passphrase string, withPasswords bool) error {
	out := make([]Connection, len(cs))
	for i, c := range cs {
		if !withPasswords {
			if c.Password != PasswordAsk { // keep the "ask" marker so an import still prompts
				c.Password = ""
			}
			if _, ok := c.Options["password"]; ok {
				o := map[string]string{}
				for k, v := range c.Options {
					o[k] = v
				}
				o["password"] = ""
				c.Options = o
			}
			if c.SSH != nil {
				s := *c.SSH
				s.Password = ""
				c.SSH = &s
			}
		}
		out[i] = c
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(connectionsFile{Connection: out}); err != nil {
		return err
	}
	if passphrase == "" {
		_, err := w.Write(buf.Bytes())
		return err
	}
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	key, err := portableKey(passphrase, salt)
	if err != nil {
		return err
	}
	blk, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(blk)
	sealed := gcm.Seal(nil, nonce, buf.Bytes(), []byte(portableMagic))
	_, err = w.Write(bytes.Join([][]byte{[]byte(portableMagic), salt, nonce, sealed}, nil))
	return err
}

// ImportConnections is ImportConnectionsOpts without allowCommands; the dropped list is discarded.
func ImportConnections(r io.Reader, passphrase string) ([]Connection, error) {
	cs, _, err := ImportConnectionsOpts(r, passphrase, false)
	return cs, err
}

// ImportConnectionsOpts reads a file written by ExportConnections and validates every connection.
// A shared file must not run programs or pick key files on this machine, so password_cmd, bootstrap,
// ssh.key and ssh.password are dropped unless allowCommands; dropped lists "name: field" for each.
func ImportConnectionsOpts(r io.Reader, passphrase string, allowCommands bool) ([]Connection, []string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	if bytes.HasPrefix(data, []byte(portableMagic)) {
		if passphrase == "" {
			return nil, nil, errors.New("this file is encrypted: a passphrase is needed")
		}
		b := data[len(portableMagic):]
		if len(b) < 16+12 {
			return nil, nil, errors.New("encrypted file is truncated")
		}
		key, err := portableKey(passphrase, b[:16])
		if err != nil {
			return nil, nil, err
		}
		blk, _ := aes.NewCipher(key)
		gcm, _ := cipher.NewGCM(blk)
		if data, err = gcm.Open(nil, b[16:28], b[28:], []byte(portableMagic)); err != nil {
			return nil, nil, errors.New("wrong passphrase or corrupt file")
		}
	}
	var f connectionsFile
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		return nil, nil, fmt.Errorf("import: %w", err)
	}
	for _, c := range f.Connection {
		if err := c.Validate(); err != nil {
			return nil, nil, fmt.Errorf("import: %w", err)
		}
	}
	var dropped []string
	if !allowCommands {
		for i := range f.Connection {
			c := &f.Connection[i]
			drop := func(field string) { dropped = append(dropped, c.Name+": "+field) }
			if c.PasswordCmd != "" {
				c.PasswordCmd = ""
				drop("password_cmd")
			}
			if len(c.Bootstrap) > 0 {
				c.Bootstrap = nil
				drop("bootstrap")
			}
			if len(c.Tools) > 0 { // a tool path runs a program on this machine
				c.Tools = nil
				drop("tools")
			}
			if c.SSH != nil && (c.SSH.Key != "" || c.SSH.Password != "") {
				s := *c.SSH
				s.Key, s.Password = "", ""
				c.SSH = &s
				drop("ssh.key/ssh.password")
			}
		}
	}
	return f.Connection, dropped, nil
}

var toolNames = []string{"pg_dump", "pg_restore", "psql", "mysqldump", "mysql", "mariadb-dump", "mariadb", "clickhouse-client", "mongodump", "mongorestore", "redis-cli"}

// ToolPathOK reports whether p may be the path of dump tool k: absolute (not UNC), no "..", and the file name is k or
// k-<version>, with .exe trimmed.
func ToolPathOK(k, p string) bool {
	if !slices.Contains(toolNames, k) || !filepath.IsAbs(p) || strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") || slices.Contains(strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(p), ".exe")
	if base == k {
		return true
	}
	v, ok := strings.CutPrefix(base, k+"-")
	return ok && v != "" && strings.Trim(v, "0123456789.") == ""
}
