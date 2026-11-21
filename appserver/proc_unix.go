//go:build unix && !linux

package appserver

import (
	"os/exec"
	"syscall"
	"time"
)

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func startProcess(cmd *exec.Cmd) (func(), error) {
	setProcAttr(cmd)
	return func() {}, cmd.Start()
}

func noteLeaderStart(p *childProc) {}

func terminateOwned(p *childProc) {
	if p == nil || p.leaderPID <= 0 {
		return
	}
	signalGroup(p.leaderPID)
}

func signalGroup(pgid int) {
	if pgid <= 0 || !processGroupExists(pgid) {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(200 * time.Millisecond)
	if processGroupExists(pgid) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

func processGroupExists(pgid int) bool {
	return syscall.Kill(-pgid, 0) == nil
}
