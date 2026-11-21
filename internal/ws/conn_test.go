package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"strings"
	"testing"
)

func TestTextRoundTrip(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveOne(right)
	}()
	client, err := Handshake(left, "mcode.local")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage([]byte(`{"ok":true}`), false); err != nil {
		t.Fatal(err)
	}
	payload, binary, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if binary || string(payload) != `{"ok":true}` {
		t.Fatalf("payload %q binary %v", payload, binary)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func serveOne(conn net.Conn) error {
	br := bufio.NewReader(conn)
	var key string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "sec-websocket-key:") {
			key = strings.TrimSpace(line[len("sec-websocket-key:"):])
		}
	}
	sum := sha1.Sum([]byte(key + acceptGUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+accept+"\r\n\r\n"); err != nil {
		return err
	}
	c := &Conn{conn: conn, r: br}
	payload, _, err := c.ReadMessage()
	if err != nil {
		return err
	}
	return c.WriteMessage(payload, false)
}
