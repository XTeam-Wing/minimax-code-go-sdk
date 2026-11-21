// Quickstart connects to a local mcode app-server over stdio and lists agents.
//
//	MCODE_BIN=mcode go run ./examples/quickstart
//
// When the executable is node, set MCODE_LEADING to the cli.js path.
// MCODE_AUTH_MODE is optional. Leave it unset for a headless API-key data
// directory. "desktop" asks for a Desktop lease and blocks SendMessage with
// 503 local_runtime_auth_pending when that lease is absent.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/appserver"
	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opt := appserver.Options{
		Command:   env("MCODE_BIN", "mcode"),
		DataDir:   os.Getenv("MCODE_DATA_DIR"),
		Workspace: os.Getenv("MCODE_WORKSPACE"),
		AuthMode:  os.Getenv("MCODE_AUTH_MODE"),
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = strings.Fields(leading)
	}
	client, err := appserver.Start(ctx, opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	fmt.Printf("server %s verified=%v\n", client.ServerVersion(), client.VersionVerified())
	if _, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{}); err != nil {
		fmt.Fprintf(os.Stderr, "list agents: %v\n%s\n", err, client.StderrTail())
		os.Exit(1)
	}
	fmt.Println("list agents completed")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
