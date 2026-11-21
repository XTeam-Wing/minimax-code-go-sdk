package ws

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCloseReturnsWhileWriteBlocks(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	errCh := make(chan error, 1)
	go func() {
		errCh <- acceptAndStop(right)
	}()
	client, err := Handshake(left, "mcode.local")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- client.WriteMessage(bytes.Repeat([]byte("x"), 256*1024), false)
	}()
	select {
	case err := <-writeDone:
		t.Fatalf("write finished early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- client.Close() }()
	select {
	case <-closeDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close did not return while a write was blocked")
	}
}

func acceptAndStop(conn net.Conn) error {
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
	_, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+accept+"\r\n\r\n")
	return err
}
