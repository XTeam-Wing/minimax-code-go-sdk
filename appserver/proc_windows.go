//go:build windows

package appserver

import (
	"os"
	"os/exec"
)

func setProcAttr(cmd *exec.Cmd) {}

func terminate(proc *os.Process) {
	if proc == nil {
		return
	}
	_ = proc.Kill()
}
