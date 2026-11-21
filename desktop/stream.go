package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// EventStream is a DesktopService SSE response. Recv returns io.EOF after
// data: [DONE] or after the tunnel closes cleanly. Cancel the context or call
// Close to abort. The tunnel is not retried.
type EventStream[T any] struct {
	method  string
	http    *HTTPStream
	parser  sseParser
	pending []sseItem
	closed  bool
}

type sseItem struct {
	raw  []byte
	done bool
	err  error
}

// Recv returns the next decoded frame.
func (s *EventStream[T]) Recv(ctx context.Context) (T, error) {
	var zero T
	if s == nil || s.closed {
		return zero, io.EOF
	}
	for {
		if len(s.pending) > 0 {
			item := s.pending[0]
			s.pending = s.pending[1:]
			if item.err != nil {
				return zero, item.err
			}
			if item.done {
				s.closed = true
				return zero, io.EOF
			}
			var out T
			if err := json.Unmarshal(item.raw, &out); err != nil {
				return zero, &DecodeError{Method: s.method, Body: item.raw, Err: err}
			}
			return out, nil
		}
		chunk, err := s.http.Recv(ctx)
		if err == io.EOF {
			s.pending = append(s.pending, s.parser.finish()...)
			if len(s.pending) == 0 {
				s.closed = true
				return zero, io.EOF
			}
			continue
		}
		if err != nil {
			return zero, err
		}
		s.pending = append(s.pending, s.parser.push(chunk)...)
	}
}

// Close stops this reader. It sends one desktop transport abort frame for
// this response. That removes the stream subscriber. It does not call
// AbortSession and it does not submit another turn. A second call is a no-op.
func (s *EventStream[T]) Close() error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	if s.http != nil {
		return s.http.Close()
	}
	return nil
}

type sseParser struct {
	buf  []byte
	data []string
}

func (p *sseParser) push(chunk []byte) []sseItem {
	p.buf = append(p.buf, chunk...)
	var items []sseItem
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			return items
		}
		line := p.buf[:i]
		p.buf = p.buf[i+1:]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if item, ok := p.consume(line); ok {
			items = append(items, item)
		}
	}
}

func (p *sseParser) finish() []sseItem {
	var items []sseItem
	if len(p.buf) > 0 {
		line := p.buf
		p.buf = nil
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if item, ok := p.consume(line); ok {
			items = append(items, item)
		}
	}
	if item, ok := p.dispatch(); ok {
		items = append(items, item)
	}
	return items
}

func (p *sseParser) consume(line []byte) (sseItem, bool) {
	if len(line) == 0 {
		return p.dispatch()
	}
	if line[0] == ':' {
		return sseItem{}, false
	}
	name, val, found := strings.Cut(string(line), ":")
	if !found {
		val = ""
	}
	if strings.HasPrefix(val, " ") {
		val = val[1:]
	}
	if name == "data" {
		p.data = append(p.data, val)
	}
	return sseItem{}, false
}

func (p *sseParser) dispatch() (sseItem, bool) {
	if len(p.data) == 0 {
		return sseItem{}, false
	}
	payload := strings.Join(p.data, "\n")
	p.data = nil
	if strings.TrimSpace(payload) == "[DONE]" {
		return sseItem{done: true}, true
	}
	return sseItem{raw: []byte(payload)}, true
}
