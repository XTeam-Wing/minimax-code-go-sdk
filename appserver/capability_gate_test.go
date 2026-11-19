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
	baseline := map[string]int{}
	for _, row := range rows {
		baseline[row.Surface]++
	}
	if baseline["idl"] != len(desktop.MethodCatalog()) || baseline["idl"] != 470 {
		t.Fatalf("idl surface %d catalog %d", baseline["idl"], len(desktop.MethodCatalog()))
	}
	if baseline["request"] != 8 || baseline["client-notification"] != 3 || baseline["server-notification"] != 3 || baseline["error"] != 11 || baseline["unix"] != 2 {
		t.Fatalf("baseline surfaces %+v", baseline)
	}
	if baseline["request"]+baseline["client-notification"]+baseline["server-notification"]+baseline["error"]+baseline["unix"]+baseline["idl"] != 497 {
		t.Fatalf("497 baseline drifted: %+v", baseline)
	}
	t.Logf("inventory=%d baseline497 extras mavis=%d event=%d blocked_policy=%d partial=true", len(rows), baseline["mavis"], baseline["event"], blocked)
	if os.Getenv("MCODE_REQUIRE_COMPLETE") == "1" {
		t.Fatal("inventory is partial: model turns, permission replies, account oauth, drive publishing, and channel send are still blocked")
	}
}

type coveragePolicy struct {
	ID      string
	Surface string
	Policy  string
	Reason  string
}

func coverageInventory() []coveragePolicy {
	blocked := blockedPolicy()
	var rows []coveragePolicy
	for _, method := range desktop.MethodCatalog() {
		row := coveragePolicy{ID: "idl." + method.Name, Surface: "idl", Policy: "server"}
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
		{ID: "request.initialize", Surface: "request", Policy: "server"},
		{ID: "request.runtime.authContext", Surface: "request", Policy: "blocked", Reason: "would inject an access token"},
		{ID: "request.account.read", Surface: "request", Policy: "server"},
		{ID: "request.account.login.start", Surface: "request", Policy: "blocked", Reason: "interactive OAuth against a dedicated account"},
		{ID: "request.account.login.cancel", Surface: "request", Policy: "blocked", Reason: "cancels an OAuth login that this process does not start"},
		{ID: "request.account.logout", Surface: "request", Policy: "blocked", Reason: "logs out the account in the data directory"},
		{ID: "request.server.prepareRestart", Surface: "request", Policy: "server"},
		{ID: "request.desktopService.transport", Surface: "request", Policy: "server"},
		{ID: "client-notification.initialized", Surface: "client-notification", Policy: "server"},
		{ID: "client-notification.runtime.authContext", Surface: "client-notification", Policy: "blocked", Reason: "would inject an access token"},
		{ID: "client-notification.desktopService.transport", Surface: "client-notification", Policy: "server"},
		{ID: "server-notification.account.login.completed", Surface: "server-notification", Policy: "blocked", Reason: "server-originated login completion"},
		{ID: "server-notification.account.updated", Surface: "server-notification", Policy: "blocked", Reason: "server-originated account change"},
		{ID: "server-notification.desktopService.transport", Surface: "server-notification", Policy: "server"},
		{ID: "error.-32700", Surface: "error", Policy: "server"},
		{ID: "error.-32600", Surface: "error", Policy: "server"},
		{ID: "error.-32601", Surface: "error", Policy: "server"},
		{ID: "error.-32602", Surface: "error", Policy: "server"},
		{ID: "error.-32603", Surface: "error", Policy: "server"},
		{ID: "error.-32001", Surface: "error", Policy: "server"},
		{ID: "error.-32002", Surface: "error", Policy: "server"},
		{ID: "error.-32003", Surface: "error", Policy: "server"},
		{ID: "error.-32004", Surface: "error", Policy: "server"},
		{ID: "error.-32005", Surface: "error", Policy: "server"},
		{ID: "error.-32010", Surface: "error", Policy: "server"},
		{ID: "unix.readyz", Surface: "unix", Policy: "server"},
		{ID: "unix.healthz", Surface: "unix", Policy: "server"},
		{ID: "mavis.health", Surface: "mavis", Policy: "server"},
		{ID: "mavis.version", Surface: "mavis", Policy: "server"},
		{ID: "mavis.runtime.status", Surface: "mavis", Policy: "server"},
		{ID: "mavis.runtime.capabilities", Surface: "mavis", Policy: "server"},
		{ID: "mavis.config.get", Surface: "mavis", Policy: "server"},
		{ID: "mavis.fs.git-info", Surface: "mavis", Policy: "server"},
		{ID: "mavis.fs.dirs", Surface: "mavis", Policy: "server"},
		{ID: "mavis.update", Surface: "mavis", Policy: "blocked", Reason: "update stub is not an updater, and this batch does not call it"},
		{ID: "mavis.communication.send", Surface: "mavis", Policy: "blocked", Reason: "would send an external message"},
		{ID: "event.permission.ask", Surface: "event", Policy: "blocked", Reason: "echo under default mode produced no permission.ask on the parent machine; that may be upstream policy. A permission test needs a command the server actually asks about, and the SDK does not auto-allow"},
		{ID: "event.subagent.execution", Surface: "event", Policy: "blocked", Reason: "parent model scenario must observe status started and read the child session"},
		{ID: "event.thread_goal", Surface: "event", Policy: "blocked", Reason: "published when a goal turn runs; parent model scenario"},
		{ID: "negative.unknown-desktop-path", Surface: "negative", Policy: "server"},
		{ID: "negative.dismiss-active-goal", Surface: "negative", Policy: "server", Reason: "DismissGoal on an active goal is HTTP 409 until the goal is complete"},
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
