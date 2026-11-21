//go:build linux

package appserver

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
}

// startProcess starts cmd on a locked OS thread. Pdeathsig watches that
// thread, and Go can exit the thread that called Start. The thread stays
// locked until release, which Close and the wait goroutine both call.
func startProcess(cmd *exec.Cmd) (func(), error) {
	releaseCh := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		setProcAttr(cmd)
		errCh <- cmd.Start()
		<-releaseCh
	}()
	if err := <-errCh; err != nil {
		close(releaseCh)
		return func() {}, err
	}
	var once sync.Once
	return func() { once.Do(func() { close(releaseCh) }) }, nil
}

func noteLeaderStart(p *childProc) {
	ticks, ok := procField(p.leaderPID, 19)
	if !ok {
		return
	}
	p.startTicks = ticks
	p.haveStart = true
}

func terminateOwned(p *childProc) {
	if p == nil || p.leaderPID <= 0 || !groupStillOurs(p) {
		return
	}
	signalGroup(p.leaderPID)
}

func signalGroup(pgid int) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	time.Sleep(200 * time.Millisecond)
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

func groupStillOurs(p *childProc) bool {
	select {
	case <-p.done:
	default:
		// Wait has not returned, so this pid is still the process we started.
		return true
	}
	if !p.haveStart {
		return false
	}
	if ticks, ok := procField(p.leaderPID, 19); ok {
		// The leader pid now belongs to a different process.
		return ticks == p.startTicks
	}
	// The leader is gone. The process group id stays reserved while any
	// descendant still has that pgrp, so signaling the group is still ours.
	return groupHasMembers(p.leaderPID)
}

func groupHasMembers(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		pgrp, ok := procField(pid, 2)
		if ok && int(pgrp) == pgid {
			return true
		}
	}
	return false
}

// procField reads a /proc/<pid>/stat field after the comm field.
// Index 2 is pgrp. Index 19 is starttime.
func procField(pid int, index int) (uint64, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 || end+2 >= len(raw) {
		return 0, false
	}
	fields := bytes.Fields(raw[end+2:])
	if index >= len(fields) {
		return 0, false
	}
	n, err := strconv.ParseUint(string(fields[index]), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
