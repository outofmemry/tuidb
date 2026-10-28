package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	tea "charm.land/bubbletea/v2"

	"webtuidb/internal/config"
	"webtuidb/internal/db"
)

// docsURL is what "Open docs" opens.
const docsURL = "https://github.com/outofmemry/webtuidb#readme"

// openURL opens a web address in the system browser (macOS open, Windows start, xdg-open elsewhere).
var openURL = func(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	return c.Start()
}

// sshLogLabel is the Open Anything entry that switches the SSH tunnel trace on or off.
func sshLogLabel() string {
	if db.SSHLog != nil {
		return "Disable SSH log"
	}
	return "Enable SSH log"
}

// openAnythingExtra runs the Open Anything entries that are not keymap actions.
func (w *Workspace) openAnythingExtra(name string) (tea.Cmd, bool) {
	switch name {
	case "open_docs":
		if err := openURL(docsURL); err != nil {
			return w.a.say("could not open a browser (%v): %s", err, docsURL), true
		}
		return w.a.say("opened %s", docsURL), true
	case "ssh_log":
		return w.toggleSSHLog(), true
	case "new_menu":
		return w.sb.newMenu(), true
	case "tab_menu":
		return w.tabMenu(w.cur), true
	}
	return nil, false
}

// toggleSSHLog turns the tunnel debug trace (db.SSHLog, the --ssh-log flag) on, writing to <data dir>/ssh.log, or off.
func (w *Workspace) toggleSSHLog() tea.Cmd {
	if db.SSHLog != nil {
		if c, ok := db.SSHLog.(interface{ Close() error }); ok {
			c.Close()
		}
		db.SSHLog = nil
		return w.a.say("SSH log off")
	}
	path := filepath.Join(config.DataDir(), "ssh.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return w.a.fail(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return w.a.fail(err)
	}
	db.SSHLog = f
	return w.a.say("SSH log on: new tunnels are traced to %s", path)
}
