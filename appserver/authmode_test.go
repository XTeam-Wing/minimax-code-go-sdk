package appserver

import (
	"strings"
	"testing"
)

func TestEmptyAuthModeOmitsFlag(t *testing.T) {
	args := commandArgs(Options{Listen: ListenStdio})
	for _, arg := range args {
		if arg == "--auth-mode" || arg == "desktop" {
			t.Fatalf("empty AuthMode injected %v", args)
		}
	}
	args = commandArgs(Options{Listen: ListenStdio, AuthMode: "desktop"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--auth-mode desktop") {
		t.Fatalf("explicit desktop missing: %v", args)
	}
	args = commandArgs(Options{Listen: ListenStdio, AuthMode: "remote-oauth"})
	if !strings.Contains(strings.Join(args, " "), "--auth-mode remote-oauth") {
		t.Fatalf("%v", args)
	}
}
