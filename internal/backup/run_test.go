package backup

import "os/exec"

func commandSh(script string) *exec.Cmd { return exec.Command("sh", "-c", script) }
