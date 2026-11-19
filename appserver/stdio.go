package appserver

import (
	"bufio"
	"io"
	"sync"
)

type stdioLink struct {
	r  *bufio.Reader
	rc io.Closer
	w  io.Writer
	c  io.Closer
	mu sync.Mutex
}

func newStdioLink(r io.Reader, w io.Writer) *stdioLink {
	var rc, c io.Closer
	if closer, ok := r.(io.Closer); ok {
		rc = closer
	}
	if closer, ok := w.(io.Closer); ok {
		c = closer
	}
	return &stdioLink{r: bufio.NewReaderSize(r, 64*1024), rc: rc, w: w, c: c}
}

func (s *stdioLink) ReadMessage() ([]byte, bool, error) {
	body, err := readLine(s.r, MaxMessageBytes)
	return body, false, err
}

func (s *stdioLink) WriteMessage(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(payload); err != nil {
		return err
	}
	_, err := s.w.Write([]byte{'\n'})
	return err
}

func (s *stdioLink) Close() error {
	var err error
	if s.rc != nil {
		err = s.rc.Close()
	}
	if s.c != nil {
		if cerr := s.c.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

func discardLine(r *bufio.Reader, frag []byte) {
	if len(frag) > 0 && frag[len(frag)-1] == '\n' {
		return
	}
	for {
		b, err := r.ReadByte()
		if err != nil || b == '\n' {
			return
		}
	}
}

func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		frag, err := r.ReadSlice('\n')
		if len(frag) > 0 {
			if len(buf)+len(frag) > max+1 {
				discardLine(r, frag)
				return nil, ErrMessageTooLarge
			}
			buf = append(buf, frag...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			if err == io.EOF && len(buf) == 0 {
				return nil, err
			}
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if len(buf) == 0 || buf[len(buf)-1] != '\n' {
			continue
		}
		buf = buf[:len(buf)-1]
		if len(buf) > 0 && buf[len(buf)-1] == '\r' {
			buf = buf[:len(buf)-1]
		}
		if len(buf) > max {
			return nil, ErrMessageTooLarge
		}
		return buf, nil
	}
}
