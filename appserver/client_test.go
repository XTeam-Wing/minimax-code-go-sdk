package appserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
	"github.com/XTeam-Wing/minimax-code-go-sdk/internal/fakeserver"
)

func pair(t *testing.T, srv *fakeserver.Server) *Client {
	t.Helper()
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	srv.In = serverRead
	srv.Out = serverWrite
	go srv.Serve()
	t.Cleanup(func() {
		_ = serverWrite.Close()
		_ = clientWrite.Close()
		_ = serverRead.Close()
		_ = clientRead.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	client, err := DialStdio(ctx, clientRead, clientWrite, Options{StreamWindow: 4})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestHandshakeAndListAgents(t *testing.T) {
	client := pair(t, &fakeserver.Server{})
	if !client.VersionVerified() {
		t.Fatalf("version %q", client.ServerVersion())
	}
	if !client.Feature("project.icon.update") {
		t.Fatal("missing default feature")
	}
	if _, err := client.Desktop().ListAgents(context.Background(), desktop.ListAgentsReq{}); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsBadProtocol(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	srv := &fakeserver.Server{In: serverRead, Out: serverWrite, RejectProtocol: true}
	go srv.Serve()
	defer serverWrite.Close()
	defer clientWrite.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := DialStdio(ctx, clientRead, clientWrite, Options{})
	if !errors.Is(err, ErrUnsupportedProtocol) {
		t.Fatalf("got %v", err)
	}
}

func TestNotInitializedAndAlreadyInitialized(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	srv := &fakeserver.Server{In: serverRead, Out: serverWrite}
	go srv.Serve()
	defer func() {
		_ = serverWrite.Close()
		_ = clientWrite.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialStdio(ctx, clientRead, clientWrite, Options{skipHandshake: true})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
	if !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("before handshake: %v", err)
	}
	if err := client.handshake(ctx); err != nil {
		t.Fatal(err)
	}
	err = client.handshake(ctx)
	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("second handshake: %v", err)
	}
}

func TestCorrelationAndNoReplay(t *testing.T) {
	var mu sync.Mutex
	var calls int
	srv := &fakeserver.Server{Handle: func(method, rawURL, body string) fakeserver.Response {
		mu.Lock()
		calls++
		mu.Unlock()
		return fakeserver.Response{Status: 200, Body: []byte(`{"url":"` + rawURL + `"}`)}
	}}
	client := pair(t, srv)
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "/minimax-desktop/api/v1/echo?i=" + string(rune('a'+i))
			resp, err := client.Desktop().RawCall(context.Background(), "GET", path, nil, nil)
			if err != nil {
				errCh <- err
				return
			}
			if !strings.Contains(string(resp.Body), path) {
				errCh <- errors.New("mismatched body " + string(resp.Body))
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 8 {
		t.Fatalf("server saw %d calls", calls)
	}
}

func TestCancelAbortsAndDoesNotReplay(t *testing.T) {
	started := make(chan struct{})
	srv := &fakeserver.Server{Handle: func(method, rawURL, body string) fakeserver.Response {
		select {
		case <-started:
		default:
			close(started)
		}
		return fakeserver.Response{Status: 200, Chunks: [][]byte{[]byte(`{"ok":true}`)}}
	}}
	// Block the first chunk by giving the server zero initial credits through a
	// normal window and holding the chunk behind a gate inside Handle's stream.
	release := make(chan struct{})
	srv.Handle = func(method, rawURL, body string) fakeserver.Response {
		close(started)
		return fakeserver.Response{Status: 200, Chunks: [][]byte{waitChunk(release)}}
	}
	client := pair(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
	urls := srv.URLs()
	if len(urls) != 1 {
		t.Fatalf("replayed or missing call: %v", urls)
	}
}

func waitChunk(release <-chan struct{}) []byte {
	<-release
	return []byte(`{}`)
}

func TestBackpressure(t *testing.T) {
	chunks := make([][]byte, 5)
	for i := range chunks {
		chunks[i] = []byte("data: {\"kind\":\"event\",\"timestamp\":1,\"type\":\"permission.ask\"}\n\n")
	}
	srv := &fakeserver.Server{Handle: func(method, rawURL, body string) fakeserver.Response {
		if strings.Contains(rawURL, "/events") {
			return fakeserver.Response{Chunks: chunks, ContentType: "text/event-stream"}
		}
		return fakeserver.Response{Body: []byte(`{}`)}
	}}
	blocked := srv.WaitCreditBlock()
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	srv.In = serverRead
	srv.Out = serverWrite
	go srv.Serve()
	defer serverWrite.Close()
	defer clientWrite.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := DialStdio(ctx, clientRead, clientWrite, Options{StreamWindow: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	stream, err := client.Desktop().SubscribeGlobalEvents(ctx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("server was not blocked on stream credit")
	}
	for i := 0; i < 5; i++ {
		frame, err := stream.Recv(ctx)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		typ, ok := frame.EventType()
		if !ok || typ != desktop.EventPermissionAsk {
			t.Fatalf("type %q ok %v", typ, ok)
		}
	}
	if _, err := stream.Recv(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("eof: %v", err)
	}
	for _, url := range srv.URLs() {
		if strings.Contains(url, "/permission/") || strings.Contains(url, "batch-reply") {
			t.Fatalf("auto approval request: %s", url)
		}
	}
}

func TestMalformedAndOversized(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	srv := &fakeserver.Server{In: serverRead, Out: serverWrite}
	go srv.Serve()
	defer clientWrite.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := DialStdio(ctx, clientRead, clientWrite, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := serverWrite.Write([]byte("{\n")); err != nil {
		t.Fatal(err)
	}
	_, err = client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
	if err == nil {
		t.Fatal("expected failure after malformed frame")
	}
}

func TestOversizedFrame(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	defer clientRead.Close()
	go func() {
		_, _ = io.WriteString(serverWrite, "{}\n")
		pad := bytes.Repeat([]byte("a"), MaxMessageBytes+8)
		_, _ = serverWrite.Write(append(pad, '\n'))
		_ = serverWrite.Close()
	}()
	link := newStdioLink(clientRead, io.Discard)
	if _, _, err := link.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	_, _, err := link.ReadMessage()
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestRedact(t *testing.T) {
	in := `{"accessToken":"super-secret","ok":true} Bearer abc.def`
	out := redact(in)
	if strings.Contains(out, "super-secret") || strings.Contains(out, "abc.def") {
		t.Fatal(out)
	}
}

func TestAuthContextRejectsLoggedOutCredentials(t *testing.T) {
	err := validateAuthContext(RuntimeAuthContext{AuthState: "logged_out", AccessToken: "x"})
	if err == nil {
		t.Fatal("expected rejection")
	}
	err = validateAuthContext(RuntimeAuthContext{AuthState: "authenticated"})
	if err == nil {
		t.Fatal("expected rejection")
	}
	if err := validateAuthContext(RuntimeAuthContext{AuthState: "authenticated", AccessToken: "t", RealUserID: "u"}); err != nil {
		t.Fatal(err)
	}
}

func TestStdioHealthUnsupported(t *testing.T) {
	client := pair(t, &fakeserver.Server{})
	if _, err := client.Readyz(context.Background()); !errors.Is(err, ErrHealthUnsupported) {
		t.Fatalf("readyz: %v", err)
	}
}
