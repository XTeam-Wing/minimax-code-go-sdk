package appserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type process interface {
	stop() error
}

type childProc struct {
	cmd        *exec.Cmd
	done       chan struct{}
	leaderPID  int
	startTicks uint64
	haveStart  bool
	release    func()
}

func (p *childProc) stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	// The leader may already have exited. Descendants in its process group
	// are still this client's processes, so stop still signals that group.
	terminateOwned(p)
	if p.release != nil {
		p.release()
	}
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
	}
	return nil
}

func rememberLeader(p *childProc) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	p.leaderPID = p.cmd.Process.Pid
	noteLeaderStart(p)
}

func launch(ctx context.Context, opt Options) (process, link, string, []string, []string, *tailWriter, error) {
	var dirs []string
	if opt.DataDir == "" {
		dir, err := os.MkdirTemp("", "mcode-data-")
		if err != nil {
			return nil, nil, "", nil, nil, nil, err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			removeOwned([]string{dir})
			return nil, nil, "", nil, nil, nil, err
		}
		opt.DataDir = dir
		dirs = append(dirs, dir)
	}
	if opt.Workspace == "" {
		dir, err := os.MkdirTemp("", "mcode-workspace-")
		if err != nil {
			removeOwned(dirs)
			return nil, nil, "", nil, nil, nil, err
		}
		opt.Workspace = dir
		dirs = append(dirs, dir)
	}
	socket := opt.SocketPath
	if opt.Listen == ListenUnix && socket == "" {
		dir, err := os.MkdirTemp("", "mcode-sock-")
		if err != nil {
			removeOwned(dirs)
			return nil, nil, "", nil, nil, nil, err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			removeOwned(append(dirs, dir))
			return nil, nil, "", nil, nil, nil, err
		}
		socket = filepath.Join(dir, "s")
		opt.SocketPath = socket
		dirs = append(dirs, dir)
	}
	argv := append([]string{opt.Command}, commandArgs(opt)...)
	cmd := exec.Command(opt.Command, commandArgs(opt)...)
	cmd.Env = opt.Env
	cmd.Dir = opt.Dir
	stderr := newTailWriter(64 * 1024)
	cmd.Stderr = io.MultiWriter(opt.Stderr, stderr)
	var lnk link
	switch opt.Listen {
	case ListenUnix:
		cmd.Stdout = io.MultiWriter(opt.Stderr, stderr)
		release, err := startProcess(cmd)
		if err != nil {
			removeOwned(dirs)
			return nil, nil, "", nil, nil, nil, err
		}
		proc := &childProc{cmd: cmd, done: make(chan struct{}), release: release}
		rememberLeader(proc)
		go func() { _ = cmd.Wait(); close(proc.done); proc.release() }()
		if err := waitHealth(ctx, socket); err != nil {
			_ = proc.stop()
			removeOwned(dirs)
			return nil, nil, "", nil, nil, stderr, fmt.Errorf("%w\nstderr:\n%s", err, redact(stderr.string()))
		}
		conn, err := dialUnix(ctx, socket)
		if err != nil {
			_ = proc.stop()
			removeOwned(dirs)
			return nil, nil, "", nil, nil, stderr, err
		}
		lnk = conn
		return proc, lnk, socket, dirs, argv, stderr, nil
	default:
		stdin, err := cmd.StdinPipe()
		if err != nil {
			removeOwned(dirs)
			return nil, nil, "", nil, nil, nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			removeOwned(dirs)
			return nil, nil, "", nil, nil, nil, err
		}
		release, err := startProcess(cmd)
		if err != nil {
			removeOwned(dirs)
			return nil, nil, "", nil, nil, nil, err
		}
		proc := &childProc{cmd: cmd, done: make(chan struct{}), release: release}
		rememberLeader(proc)
		go func() { _ = cmd.Wait(); close(proc.done); proc.release() }()
		return proc, newStdioLink(stdout, stdin), "", dirs, argv, stderr, nil
	}
}

func waitHealth(ctx context.Context, socket string) error {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		status, err := getHealth(ctx, socket, "/readyz")
		if err == nil && status.OK {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if err == nil {
				err = fmt.Errorf("readyz ok=%v", status.OK)
			}
			return fmt.Errorf("appserver: timed out waiting for %s: %w", socket, err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func removeAll(path string) error { return os.RemoveAll(path) }

func removeOwned(dirs []string) {
	for _, dir := range dirs {
		_ = os.RemoveAll(dir)
	}
}
