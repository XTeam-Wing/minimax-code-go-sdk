// Package fakeserver is an in-process mcode app-server for protocol tests.
package fakeserver

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"sync"
)

// Response is a scripted DesktopService HTTP response.
type Response struct {
	Status      int
	Body        []byte
	ContentType string
	// Chunks, when set, are sent as tunnel stream-chunk payloads instead of Body.
	// The server obeys streamWindow credits.
	Chunks [][]byte
}

// Server speaks the outer JSON-RPC session and a scripted desktop tunnel.
type Server struct {
	In  io.Reader
	Out io.Writer

	// Handle maps an HTTP method and URL to a response. Nil selects 200 {}.
	Handle func(method, rawURL string, body string) Response

	// RejectProtocol makes initialize return unsupportedProtocol.
	RejectProtocol bool

	mu       sync.Mutex
	writeMu  sync.Mutex
	credits  map[string]int
	waiters  map[string]chan struct{}
	requests []recorded
	ready    chan struct{}
	once     sync.Once
	sawInit  bool
	sawNoted bool
}

type recorded struct {
	Method string
	URL    string
	Body   string
}

// Requests returns desktop request frames received so far.
func (s *Server) Requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recorded, len(s.requests))
	copy(out, s.requests)
	return out
}

// URLs lists desktop request URLs.
func (s *Server) URLs() []string {
	reqs := s.Requests()
	out := make([]string, len(reqs))
	for i, req := range reqs {
		out[i] = req.Method + " " + req.URL
	}
	return out
}

// WaitCreditBlock reports when a stream is waiting because its credits are zero.
func (s *Server) WaitCreditBlock() <-chan struct{} {
	s.once.Do(func() { s.ready = make(chan struct{}) })
	return s.ready
}

// Serve reads until the input ends.
func (s *Server) Serve() {
	s.mu.Lock()
	if s.credits == nil {
		s.credits = map[string]int{}
		s.waiters = map[string]chan struct{}{}
	}
	s.mu.Unlock()
	in := bufio.NewReaderSize(s.In, 64*1024)
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			s.dispatch(line)
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) dispatch(line []byte) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		s.reply(map[string]any{"id": nil, "error": rpcError(-32700, "Invalid JSON.")})
		return
	}
	var method string
	_ = json.Unmarshal(msg["method"], &method)
	var id any
	if raw, ok := msg["id"]; ok {
		_ = json.Unmarshal(raw, &id)
	}
	_, hasID := msg["id"]
	switch {
	case method == "initialize" && hasID:
		if s.sawInit {
			s.reply(map[string]any{"id": id, "error": rpcError(-32003, "The app-server connection is already initialized.")})
			return
		}
		if s.RejectProtocol {
			s.reply(map[string]any{"id": id, "error": rpcError(-32005, "protocol mismatch")})
			return
		}
		var params struct {
			ProtocolVersion int `json:"protocolVersion"`
			Capabilities    struct {
				DesktopService struct {
					TransportProtocolVersion int `json:"transportProtocolVersion"`
				} `json:"desktopService"`
			} `json:"capabilities"`
		}
		_ = json.Unmarshal(msg["params"], &params)
		if params.ProtocolVersion != 1 {
			s.reply(map[string]any{"id": id, "error": rpcError(-32005, "protocol mismatch")})
			return
		}
		if params.Capabilities.DesktopService.TransportProtocolVersion != 2 {
			s.reply(map[string]any{"id": id, "error": rpcError(-32004, "transport mismatch")})
			return
		}
		s.sawInit = true
		s.reply(map[string]any{"id": id, "result": map[string]any{
			"protocolVersion": 1,
			"serverInfo":      map[string]any{"name": "mcode-app-server", "title": "fake", "version": "0.7.8"},
			"workspaceDir":    "/tmp/workspace",
			"capabilities": map[string]any{
				"methods":             []string{"desktopService/transport"},
				"clientNotifications": []string{"initialized", "desktopService/transport"},
				"serverNotifications": []string{"desktopService/transport"},
				"transports":          []string{"stdio", "unix-websocket"},
				"features":            []string{"project.icon.update"},
				"desktopService": map[string]any{
					"contract":                        "DesktopService",
					"transportProtocolVersion":        2,
					"negotiatedBinaryPayloadEncoding": "base64",
					"defaultStreamWindow":             16,
					"maxStreamWindow":                 256,
				},
				"reconnect": map[string]any{"history": true},
				"auth":      map[string]any{"mode": "remote-oauth"},
			},
		}})
	case method == "initialized":
		s.sawNoted = true
		return
	case hasID && !s.sawNoted:
		s.reply(map[string]any{"id": id, "error": rpcError(-32002, "Send the initialized notification before app-server requests.")})
		return
	case method == "desktopService/transport" && !hasID:
		s.onClientFrame(msg["params"])
	case method == "desktopService/transport" && hasID:
		s.onDesktopRequest(id, msg["params"])
	case method != "" && hasID:
		s.reply(map[string]any{"id": id, "error": rpcError(-32601, "Unknown app-server method: "+method)})
	default:
	}
}

func (s *Server) onDesktopRequest(id any, params json.RawMessage) {
	var envelope struct {
		Frame struct {
			Type         string          `json:"type"`
			ID           string          `json:"id"`
			Method       string          `json:"method"`
			URL          string          `json:"url"`
			Body         string          `json:"body"`
			StreamWindow json.RawMessage `json:"streamWindow"`
		} `json:"frame"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil || envelope.Frame.Type != "request" {
		s.reply(map[string]any{"id": id, "error": rpcError(-32602, "invalid frame")})
		return
	}
	frame := envelope.Frame
	s.mu.Lock()
	s.requests = append(s.requests, recorded{Method: frame.Method, URL: frame.URL, Body: frame.Body})
	window := 0
	if len(frame.StreamWindow) > 0 && string(frame.StreamWindow) != "null" {
		n, _ := strconv.Atoi(string(frame.StreamWindow))
		window = n
	}
	if window == 0 {
		window = 1 << 30
	}
	s.credits[frame.ID] = window
	s.mu.Unlock()
	s.reply(map[string]any{"id": id, "result": map[string]any{"accepted": true, "requestId": frame.ID}})
	resp := Response{Status: 200, Body: []byte(`{}`), ContentType: "application/json"}
	if s.Handle != nil {
		resp = s.Handle(frame.Method, frame.URL, frame.Body)
	}
	if resp.Status == 0 {
		resp.Status = 200
	}
	if resp.ContentType == "" {
		if len(resp.Chunks) > 0 {
			resp.ContentType = "text/event-stream"
		} else {
			resp.ContentType = "application/json"
		}
	}
	s.frame(map[string]any{
		"type": "response-start", "id": frame.ID, "seq": 0, "status": resp.Status,
		"headers": map[string]string{"content-type": resp.ContentType},
	})
	chunks := resp.Chunks
	if chunks == nil {
		chunks = [][]byte{resp.Body}
	}
	go s.stream(frame.ID, chunks)
}

func (s *Server) stream(id string, chunks [][]byte) {
	for i, chunk := range chunks {
		if !s.acquire(id) {
			return
		}
		s.frame(map[string]any{
			"type": "stream-chunk", "id": id, "seq": i + 1, "payload": string(chunk),
		})
	}
	s.frame(map[string]any{"type": "stream-done", "id": id, "seq": len(chunks) + 1})
}

func (s *Server) acquire(id string) bool {
	s.mu.Lock()
	for s.credits[id] <= 0 {
		if s.ready != nil {
			select {
			case <-s.ready:
			default:
				close(s.ready)
			}
		}
		ch := s.waiters[id]
		if ch == nil {
			ch = make(chan struct{})
			s.waiters[id] = ch
		}
		s.mu.Unlock()
		<-ch
		s.mu.Lock()
	}
	s.credits[id]--
	s.mu.Unlock()
	return true
}

func (s *Server) onClientFrame(params json.RawMessage) {
	var envelope struct {
		Frame struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Credits int    `json:"credits"`
		} `json:"frame"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return
	}
	switch envelope.Frame.Type {
	case "stream-credit":
		s.mu.Lock()
		s.credits[envelope.Frame.ID] += envelope.Frame.Credits
		if ch := s.waiters[envelope.Frame.ID]; ch != nil {
			close(ch)
			delete(s.waiters, envelope.Frame.ID)
		}
		s.mu.Unlock()
	case "abort":
		s.mu.Lock()
		s.credits[envelope.Frame.ID] = 1 << 30
		if ch := s.waiters[envelope.Frame.ID]; ch != nil {
			close(ch)
			delete(s.waiters, envelope.Frame.ID)
		}
		s.mu.Unlock()
	default:
	}
}

func (s *Server) frame(frame map[string]any) {
	s.reply(map[string]any{"method": "desktopService/transport", "params": map[string]any{"frame": frame}})
}

func (s *Server) reply(msg map[string]any) {
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	body = append(body, '\n')
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = s.Out.Write(body)
}

func rpcError(code int, message string) map[string]any {
	return map[string]any{"code": code, "message": message}
}
