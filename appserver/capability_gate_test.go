package appserver

import (
	"os"
	"testing"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// TestCapabilityGate checks that the denominator lists every generated IDL
// method plus the outer RPC, mavis, and event rows known in this client.
// It does not store the latest run's pass count. Run TestLiveLocalBatch to
// write that report outside the repository.
func TestCapabilityGate(t *testing.T) {
	rows := coverageInventory()
	seen := map[string]bool{}
	var blocked int
	for _, row := range rows {
		if row.ID == "" || seen[row.ID] {
			t.Fatalf("bad id %q", row.ID)
		}
		seen[row.ID] = true
		if row.Policy == "blocked" {
			blocked++
			if row.Reason == "" {
				t.Fatalf("%s blocked without a reason", row.ID)
			}
		}
	}
	for _, method := range desktop.MethodCatalog() {
		if !seen["idl."+method.Name] {
			t.Fatalf("inventory missing %s", method.Name)
		}
	}
	if len(rows) < len(desktop.MethodCatalog()) {
		t.Fatalf("inventory %d catalog %d", len(rows), len(desktop.MethodCatalog()))
	}
	t.Logf("inventory=%d catalog=%d blocked_policy=%d partial=true", len(rows), len(desktop.MethodCatalog()), blocked)
	if os.Getenv("MCODE_REQUIRE_COMPLETE") == "1" {
		t.Fatal("inventory is partial: model turns, permission replies, account oauth, drive publishing, and channel send are still blocked")
	}
}

type coveragePolicy struct {
	ID     string
	Policy string
	Reason string
}

func coverageInventory() []coveragePolicy {
	blocked := blockedPolicy()
	var rows []coveragePolicy
	for _, method := range desktop.MethodCatalog() {
		row := coveragePolicy{ID: "idl." + method.Name, Policy: "server"}
		if reason, ok := blocked[method.Name]; ok {
			row.Policy = "blocked"
			row.Reason = reason
		} else if method.GatewayOverride {
			row.Policy = "blocked"
			row.Reason = "drive gateway can publish or mutate a remote site"
		} else if method.SideEffect == "auth_credential" {
			row.Policy = "blocked"
			row.Reason = "credential write or secret read"
		} else if method.SideEffect == "approval" {
			row.Policy = "blocked"
			row.Reason = "permission reply is not auto-approved"
		}
		rows = append(rows, row)
	}
	rows = append(rows, []coveragePolicy{
		{ID: "rpc.initialize", Policy: "server"},
		{ID: "rpc.initialized", Policy: "server"},
		{ID: "rpc.account.read", Policy: "server"},
		{ID: "rpc.account.login.start", Policy: "blocked", Reason: "interactive OAuth against a dedicated account"},
		{ID: "rpc.account.login.cancel", Policy: "blocked", Reason: "cancels an OAuth login that this process does not start"},
		{ID: "rpc.account.logout", Policy: "blocked", Reason: "logs out the account in the data directory"},
		{ID: "rpc.runtime.authContext", Policy: "blocked", Reason: "would inject an access token"},
		{ID: "rpc.server.prepareRestart", Policy: "server"},
		{ID: "notify.initialized", Policy: "server"},
		{ID: "notify.runtime.authContext", Policy: "blocked", Reason: "would inject an access token"},
		{ID: "notify.account.login.completed", Policy: "blocked", Reason: "server-originated; not synthesized"},
		{ID: "notify.account.updated", Policy: "blocked", Reason: "server-originated account change"},
		{ID: "mavis.health", Policy: "server"},
		{ID: "mavis.version", Policy: "server"},
		{ID: "mavis.runtime.status", Policy: "server"},
		{ID: "mavis.runtime.capabilities", Policy: "server"},
		{ID: "mavis.config.get", Policy: "server"},
		{ID: "mavis.fs.git-info", Policy: "server"},
		{ID: "mavis.fs.dirs", Policy: "server"},
		{ID: "mavis.update", Policy: "blocked", Reason: "update stub is not an updater, and this batch does not call it"},
		{ID: "mavis.communication.send", Policy: "blocked", Reason: "would send an external message"},
		{ID: "event.permission.ask", Policy: "blocked", Reason: "needs a real tool ask; replies are not automatic"},
		{ID: "event.subagent.execution", Policy: "blocked", Reason: "parent model scenario must observe status started and read the child session"},
		{ID: "event.thread_goal", Policy: "blocked", Reason: "published when a goal turn runs; parent model scenario"},
		{ID: "negative.unknown-desktop-path", Policy: "server"},
	}...)
	return rows
}

func blockedPolicy() map[string]string {
	return map[string]string{
		"SendMessage":           "model turn requires the parent API key and MCODE_E2E_LLM=1",
		"ReplyPermission":       "needs a real permission.ask; the SDK must not auto-allow",
		"BatchReplyPermissions": "needs a real permission.ask batch; the SDK must not auto-allow",
		"UpsertMiniMaxApiKey":   "would store a provider credential",
		"PrepareRestart":        "restarts the owned server; covered only by a dedicated lifecycle test when enabled",
	}
}
