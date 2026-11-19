package appserver

import (
	"regexp"
	"sync"
)

type tailWriter struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailWriter(max int) *tailWriter {
	if max <= 0 {
		max = 64 * 1024
	}
	return &tailWriter{max: max}
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.max:]...)
	}
	return len(p), nil
}

func (w *tailWriter) string() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

var (
	redactJSON   = regexp.MustCompile(`(?i)("(?:accessToken|access_token|refreshToken|refresh_token|apiKey|api_key|clientSecret|client_secret|authorization|token)"\s*:\s*")[^"]*(")`)
	redactBearer = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._\-+/=]+`)
	redactKey    = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9]{8})[A-Za-z0-9]+`)
)

func redact(s string) string {
	s = redactJSON.ReplaceAllString(s, `${1}[redacted]$2`)
	s = redactBearer.ReplaceAllString(s, `${1}[redacted]`)
	s = redactKey.ReplaceAllString(s, `${1}[redacted]`)
	return s
}
