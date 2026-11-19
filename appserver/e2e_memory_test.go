package appserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func TestLiveMemoryEnvelope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	disabled := startMemory(t, false)
	name := "mavis"
	disabledResp, err := disabled.Desktop().AppendMemory(ctx, desktop.AppendMemoryReq{Content: "disabled", AgentName: &name})
	var biz *desktop.BusinessError
	if !errors.As(err, &biz) || biz.Code != "MEMORY_DISABLED" || biz.StatusCode != 1 || biz.Message == "" {
		t.Fatalf("disabled append: %v %#v", err, biz)
	}
	if disabledResp.BaseResp == nil || !strings.Contains(string(biz.Body), "MEMORY_DISABLED") {
		t.Fatalf("disabled payload %#v %s", disabledResp.BaseResp, biz.Body)
	}

	enabled := startMemory(t, true)
	marker := "sdk-memory-marker"
	body := marker + "\n" + marker + "\n"
	if _, err := enabled.Desktop().AppendMemory(ctx, desktop.AppendMemoryReq{Content: body, AgentName: &name}); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := enabled.Desktop().GetMemory(ctx, desktop.GetMemoryReq{AgentName: &name})
	if err != nil || got.Content == nil {
		t.Fatalf("get: %v", err)
	}
	text, ok := got.Content.String()
	if !ok || !strings.Contains(text, marker) {
		t.Fatalf("content %q", text)
	}
	searched, err := enabled.Desktop().SearchMemory(ctx, desktop.SearchMemoryReq{Query: marker, AgentName: &name})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !searchHasLine(searched, marker) {
		t.Fatalf("search results %#v", searched.Results)
	}
	_, err = enabled.Desktop().EditMemory(ctx, desktop.EditMemoryReq{OldString: marker, NewString: "sdk-memory-once", AgentName: &name})
	biz = nil
	if !errors.As(err, &biz) || biz.Code != "OLD_STRING_AMBIGUOUS" || biz.StatusCode != 1 {
		t.Fatalf("ambiguous edit: %v %#v", err, biz)
	}
	replaceAll := true
	edited, err := enabled.Desktop().EditMemory(ctx, desktop.EditMemoryReq{OldString: marker, NewString: "sdk-memory-once", ReplaceAll: &replaceAll, AgentName: &name})
	if err != nil {
		t.Fatalf("replace all: %v", err)
	}
	count, ok := edited.Replacements.Float64()
	if !ok || count < 1 {
		t.Fatalf("replacements %#v", edited.Replacements)
	}
	after, err := enabled.Desktop().GetMemory(ctx, desktop.GetMemoryReq{AgentName: &name})
	if err != nil || after.Content == nil {
		t.Fatalf("get after edit: %v", err)
	}
	text, ok = after.Content.String()
	if !ok || strings.Contains(text, marker) || !strings.Contains(text, "sdk-memory-once") {
		t.Fatalf("edited content %q", text)
	}
}

func searchHasLine(resp desktop.SearchMemoryResp, marker string) bool {
	for _, result := range resp.Results {
		line, ok := result.Line.String()
		if ok && strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func startMemory(t *testing.T, enabled bool) *Client {
	t.Helper()
	if os.Getenv("MCODE_E2E") != "1" {
		t.Skip("set MCODE_E2E=1 to run the official app-server")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	data := t.TempDir()
	workspace := t.TempDir()
	flag := "false"
	if enabled {
		flag = "true"
	}
	if err := os.WriteFile(filepath.Join(data, "config.yaml"), []byte("memory:\n  enabled: "+flag+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	opt := Options{
		Command:   bin,
		DataDir:   data,
		Workspace: workspace,
		AuthMode:  "desktop",
		Listen:    ListenStdio,
		Stderr:    &testLog{t},
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, stderrOf(client))
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
