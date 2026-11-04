//go:build !windows

package live

import "os/exec"

func hideConsole(cmd *exec.Cmd) {}
