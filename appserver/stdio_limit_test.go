package appserver

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestReadLineAllowsMaxPayloadWithCRLF(t *testing.T) {
	const max = 8
	payload := strings.Repeat("a", max)
	input := append([]byte(payload), '\r', '\n')
	got, err := readLine(bufio.NewReader(bytes.NewReader(input)), max)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("got %q", got)
	}
}

func TestReadLineRejectsPayloadPastMax(t *testing.T) {
	const max = 8
	input := append(bytes.Repeat([]byte("a"), max+1), '\n')
	if _, err := readLine(bufio.NewReader(bytes.NewReader(input)), max); err == nil {
		t.Fatal("expected limit error")
	}
}
