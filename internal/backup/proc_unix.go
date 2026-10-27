//go:build !windows

package backup

import (
	"os/exec"
	"syscall"
)

// inOwnGroup makes cmd a process-group leader so a cancel can take its children down too.
func inOwnGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) != nil {
		_ = cmd.Process.Kill()
	}
}
