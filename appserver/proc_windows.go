//go:build windows

package appserver

import (
	"os/exec"
)

func setProcAttr(cmd *exec.Cmd) {}

func startProcess(cmd *exec.Cmd) (func(), error) {
	return func() {}, cmd.Start()
}

func noteLeaderStart(p *childProc) {}

func terminateOwned(p *childProc) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
}
