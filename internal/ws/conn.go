// Package ws is a small WebSocket client for the app-server unix listener.
// It implements the RFC 6455 framing this server uses: one JSON-RPC text
// message per frame, plus optional binary runtime frames.
package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const acceptGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// MaxMessageBytes matches the app-server limit.
const MaxMessageBytes = 16 * 1024 * 1024

// Conn is a client WebSocket connection.
type Conn struct {
	conn net.Conn
	r    *bufio.Reader
	mu   sync.Mutex
	once sync.Once
}

// Handshake performs the HTTP upgrade. The request has no Origin header,
// which the app-server rejects.
func Handshake(conn net.Conn, host string) (*Conn, error) {
	if host == "" {
		host = "mcode.local"
	}
	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 64*1024)
	status, headers, err := readHandshake(br)
	if err != nil {
		return nil, err
	}
	if status != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("ws: upgrade status %d", status)
	}
	if !strings.EqualFold(headerValue(headers, "Upgrade"), "websocket") {
		return nil, errors.New("ws: missing websocket upgrade")
	}
	sum := sha1.Sum([]byte(key + acceptGUID))
	want := base64.StdEncoding.EncodeToString(sum[:])
	if headerValue(headers, "Sec-WebSocket-Accept") != want {
		return nil, errors.New("ws: accept key mismatch")
	}
	return &Conn{conn: conn, r: br}, nil
}

func readHandshake(r *bufio.Reader) (int, map[string]string, error) {
	statusLine, err := r.ReadString('\n')
	if err != nil {
		return 0, nil, err
	}
	parts := strings.Split(strings.TrimSpace(statusLine), " ")
	if len(parts) < 2 {
		return 0, nil, errors.New("ws: malformed status line")
	}
	var status int
	if _, err := fmt.Sscanf(parts[1], "%d", &status); err != nil {
		return 0, nil, err
	}
	headers := map[string]string{}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return 0, nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return status, headers, nil
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return 0, nil, errors.New("ws: malformed header")
		}
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
}

func headerValue(headers map[string]string, name string) string {
	return headers[strings.ToLower(name)]
}

// ReadMessage returns the next data frame. binary is true for opcode 2.
func (c *Conn) ReadMessage() (payload []byte, binary bool, err error) {
	var assembled []byte
	var kind byte
	for {
		fin, opcode, data, err := c.readFrame()
		if err != nil {
			return nil, false, err
		}
		switch opcode {
		case 0x1, 0x2:
			kind = opcode
			assembled = data
		case 0x0:
			assembled = append(assembled, data...)
		case 0x8:
			return nil, false, io.EOF
		case 0x9:
			_ = c.writeFrame(0xA, data)
			continue
		case 0xA:
			continue
		default:
			return nil, false, fmt.Errorf("ws: unsupported opcode %d", opcode)
		}
		if len(assembled) > MaxMessageBytes {
			return nil, false, errors.New("ws: message exceeds 16MiB limit")
		}
		if fin {
			return assembled, kind == 0x2, nil
		}
	}
}

// WriteMessage sends one data frame. The payload is masked.
func (c *Conn) WriteMessage(payload []byte, binary bool) error {
	opcode := byte(0x1)
	if binary {
		opcode = 0x2
	}
	return c.writeFrame(opcode, payload)
}

// Close sends a close frame and closes the underlying connection.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		_ = c.writeFrame(0x8, []byte{0x03, 0xe8})
		err = c.conn.Close()
	})
	return err
}

func (c *Conn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err = io.ReadFull(c.r, header); err != nil {
		return false, 0, nil, err
	}
	fin = header[0]&0x80 != 0
	opcode = header[0] & 0x0f
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(c.r, ext); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(c.r, ext); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	if length > MaxMessageBytes {
		return false, 0, nil, errors.New("ws: frame exceeds 16MiB limit")
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.r, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(c.r, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return fin, opcode, payload, nil
}

func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(payload) > MaxMessageBytes {
		return errors.New("ws: frame exceeds 16MiB limit")
	}
	header := []byte{0x80 | opcode, 0x80}
	length := len(payload)
	switch {
	case length < 126:
		header[1] |= byte(length)
	case length <= 65535:
		header[1] |= 126
		header = append(header, byte(length>>8), byte(length))
	default:
		header[1] |= 127
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(length))
		header = append(header, ext...)
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}
