//go:build windows

package backup

import "os/exec"

func inOwnGroup(*exec.Cmd) {}

// shortcut: only the direct child is killed on Windows; upgrade to a job object for grandchildren.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
