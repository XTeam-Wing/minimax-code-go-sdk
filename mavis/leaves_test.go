package mavis

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

type captureRT struct {
	req desktop.HTTPRequest
}

func (c *captureRT) RoundTrip(ctx context.Context, req desktop.HTTPRequest) (desktop.HTTPResponse, error) {
	c.req = req
	return desktop.HTTPResponse{Status: 200, Body: []byte(`{"success":true,"tasks":[],"count":0,"isGitWorkspace":false,"isInsideWorktree":false,"hasHead":false}`)}, nil
}

func (c *captureRT) RoundTripStream(ctx context.Context, req desktop.HTTPRequest) (*desktop.HTTPStream, error) {
	return nil, errors.New("unexpected stream")
}

func TestFileTreeQuery(t *testing.T) {
	rt := &captureRT{}
	client := New(desktop.New(rt))
	if _, err := client.FileTree(context.Background(), WorkspaceQuery{Workspace: "/work", Path: "a b"}); err != nil {
		t.Fatal(err)
	}
	if rt.req.Method != "GET" || !strings.Contains(rt.req.URL, "/mavis/api/file/tree") {
		t.Fatalf("url %s %s", rt.req.Method, rt.req.URL)
	}
	if !strings.Contains(rt.req.URL, "workspace=%2Fwork") || !strings.Contains(rt.req.URL, "path=a+b") {
		t.Fatalf("query %s", rt.req.URL)
	}
}

func TestReplyPermissionsRejectsEmptyDecision(t *testing.T) {
	rt := &captureRT{}
	client := New(desktop.New(rt))
	_, err := client.ReplyPermissions(context.Background(), BatchReply{Decision: ""})
	if !errors.Is(err, errDecision) {
		t.Fatal(err)
	}
	if rt.req.URL != "" {
		t.Fatalf("sent %s", rt.req.URL)
	}
}

func TestGitProbePath(t *testing.T) {
	rt := &captureRT{}
	client := New(desktop.New(rt))
	if _, err := client.GitProbe(context.Background(), WorkspaceQuery{Workspace: "/repo"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rt.req.URL, "/mavis/api/file/git/probe") {
		t.Fatal(rt.req.URL)
	}
}

func TestFsGitInfo(t *testing.T) {
	rt := &captureRT{}
	client := New(desktop.New(rt))
	info, err := client.FsGitInfo(context.Background(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if info.IsGitWorkspace || info.HasHead {
		t.Fatalf("%+v", info)
	}
	if !strings.Contains(rt.req.URL, "/mavis/api/fs/git-info") || !strings.Contains(rt.req.URL, "path=%2Frepo") {
		t.Fatal(rt.req.URL)
	}
	_ = io.EOF
}
