//go:build unix

package appserver

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"testing"
	"time"
)

type nopLink struct{}

func (nopLink) ReadMessage() ([]byte, bool, error) { return nil, false, io.EOF }
func (nopLink) WriteMessage([]byte) error          { return nil }
func (nopLink) Close() error                       { return nil }

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			var pid int
			if _, scanErr := fmt.Sscanf(string(raw), "%d", &pid); scanErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}

func TestCloseKillsGroupAfterLeaderExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are a Unix behavior")
	}
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grand.pid")
	script := filepath.Join(dir, "leader.sh")
	body := "#!/bin/sh\nsleep 30 >/dev/null 2>&1 &\necho $! > \"$PIDFILE\"\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(script)
	cmd.Env = append(os.Environ(), "PIDFILE="+pidFile)
	release, err := startProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	proc := &childProc{cmd: cmd, done: make(chan struct{}), release: release}
	rememberLeader(proc)
	go func() { _ = cmd.Wait(); close(proc.done); proc.release() }()
	grand := readPIDFile(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(grand, syscall.SIGKILL) })
	select {
	case <-proc.done:
	case <-time.After(2 * time.Second):
		t.Fatal("leader did not exit")
	}
	client := newClient(Options{}, nopLink{}, proc)
	done := make(chan struct{})
	go func() {
		_ = client.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
	time.Sleep(200 * time.Millisecond)
	if processAlive(grand) {
		t.Fatalf("grandchild %d still alive after Close", grand)
	}
}

func TestLinuxChildStaysAliveWhileOwnerLives(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Pdeathsig is Linux-only")
	}
	cmd := exec.Command("sleep", "30")
	started := make(chan error, 1)
	var release func()
	go func() {
		var err error
		release, err = startProcess(cmd)
		started <- err
	}()
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		if release != nil {
			release()
		}
	})
	runtime.GC()
	debug.FreeOSMemory()
	time.Sleep(500 * time.Millisecond)
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("child exited while the owner process was still alive")
	}
}

func TestLinuxChildDiesWhenOwnerProcessExits(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Pdeathsig is Linux-only")
	}
	if os.Getenv("MCODE_PARENT_EXIT") == "1" {
		cmd := exec.Command("sleep", "30")
		setProcAttr(cmd)
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%d\n", cmd.Process.Pid)
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLinuxChildDiesWhenOwnerProcessExits")
	cmd.Env = append(os.Environ(), "MCODE_PARENT_EXIT=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Sscanf(string(out), "%d", &pid); err != nil || pid <= 0 {
		t.Fatalf("pid %q", out)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	time.Sleep(300 * time.Millisecond)
	if processAlive(pid) {
		t.Fatalf("child %d survived the owner process", pid)
	}
}
