package appserver

import (
	"os"
	"testing"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// coverageRow is one reachable capability. Class is the kind of evidence a
// test is allowed to claim. fakecontract is the generated wrapper test and is
// not a server pass. blocked is a required case this process must not pretend
// to have run.
type coverageRow struct {
	ID     string
	Class  string
	Test   string
	Reason string
}

func TestCapabilityGate(t *testing.T) {
	rows := coverageRows()
	if len(rows) < 470 {
		t.Fatalf("denominator %d", len(rows))
	}
	seen := map[string]bool{}
	var real, negative, fake, blocked int
	for _, row := range rows {
		if row.ID == "" || seen[row.ID] {
			t.Fatalf("bad id %q", row.ID)
		}
		seen[row.ID] = true
		switch row.Class {
		case "real":
			real++
			if row.Test == "" {
				t.Fatalf("%s real row has no test", row.ID)
			}
		case "negative":
			negative++
			if row.Test == "" {
				t.Fatalf("%s negative row has no test", row.ID)
			}
		case "fakecontract":
			fake++
		case "blocked":
			blocked++
			if row.Reason == "" {
				t.Fatalf("%s blocked row has no reason", row.ID)
			}
		default:
			t.Fatalf("%s class %s", row.ID, row.Class)
		}
	}
	t.Logf("capabilities=%d real=%d negative=%d fakecontract=%d blocked=%d", len(rows), real, negative, fake, blocked)
	if os.Getenv("MCODE_REQUIRE_COMPLETE") == "1" && (fake > 0 || blocked > 0) {
		t.Fatalf("not complete: real=%d negative=%d fakecontract=%d blocked=%d", real, negative, fake, blocked)
	}
}

func coverageRows() []coverageRow {
	overrides := coverageOverrides()
	var rows []coverageRow
	for _, method := range desktop.MethodCatalog() {
		row, ok := overrides[method.Name]
		if !ok {
			row = coverageRow{Class: "fakecontract", Test: "TestEveryWrapperMatchesInventory"}
		}
		row.ID = "idl." + method.Name
		rows = append(rows, row)
		delete(overrides, method.Name)
	}
	for name, row := range overrides {
		if row.ID == "" {
			row.ID = "extra." + name
		}
		rows = append(rows, row)
	}
	rows = append(rows, outerCoverageRows()...)
	return rows
}

func coverageOverrides() map[string]coverageRow {
	real := func(test string) coverageRow {
		return coverageRow{Class: "real", Test: test}
	}
	return map[string]coverageRow{
		"ListAgents":            real("TestLiveSurface"),
		"CreateSession":         real("TestLiveSurface"),
		"GetSession":            real("TestLiveSurface"),
		"UpdateSession":         {Class: "negative", Test: "TestLiveSurface", Reason: "0.7.8 returns HTTP 422 CONTENT_POLICY_VIOLATION for a title change; that is not a successful rename"},
		"ListSessions":          real("TestLiveSurface"),
		"AbortSession":          real("TestLiveSurface"),
		"GetEcho":               real("TestLiveSurface"),
		"Ping":                  real("TestLiveSurface"),
		"SubscribeGlobalEvents": real("TestLiveSurface"),
		"ListWorkspaceFileTree": real("TestLiveSurface"),
		"CreateGoal":            real("TestLiveSurface"),
		"GetGoal":               real("TestLiveSurface"),
		"DeleteGoal":            real("TestLiveSurface"),
		"SendMessage":           {Class: "blocked", Test: "TestLiveModelTurn", Reason: "model turn requires the parent API key; MCODE_E2E_LLM=1 and MCODE_LLM_DATA_DIR; AuthMode must be empty"},
		"ReplyPermission":       {Class: "blocked", Reason: "needs a real permission.ask from a scoped test tool; the SDK must not auto-allow"},
		"BatchReplyPermissions": {Class: "blocked", Reason: "needs a real permission.ask batch; the SDK must not auto-allow"},
		"AccountLoginStart":     {ID: "extra.AccountLoginStart", Class: "blocked", Reason: "interactive OAuth against a dedicated account; not started by the SDK"},
		"UpsertMiniMaxApiKey":   {Class: "blocked", Reason: "would write a credential into the data directory"},
		"PrepareRestart":        {Class: "blocked", Reason: "restarts the server process; not part of the isolated read and session lifecycle"},
	}
}

func outerCoverageRows() []coverageRow {
	return []coverageRow{
		{ID: "rpc.initialize", Class: "real", Test: "TestLiveSurface"},
		{ID: "rpc.initialized", Class: "real", Test: "TestLiveSurface"},
		{ID: "rpc.account.read", Class: "negative", Test: "TestLiveSurface", Reason: "0.7.8 app-server answers -32601 unknown method; that is not an account read"},
		{ID: "rpc.account.login.start", Class: "blocked", Reason: "interactive OAuth against a dedicated account"},
		{ID: "rpc.account.logout", Class: "blocked", Reason: "logs out the account in the data directory"},
		{ID: "mavis.health", Class: "real", Test: "TestLiveSurface"},
		{ID: "negative.unknown-desktop-path", Class: "negative", Test: "TestLiveSurface"},
		{ID: "event.subagent.execution", Class: "blocked", Test: "TestLiveGoalAndSubagent", Reason: "a subagent counts only after subagent.execution status started and GetSession on the child; a prompt that mentions a subagent is not evidence"},
		{ID: "drive.gateway", Class: "blocked", Reason: "website gateway calls can publish or mutate remote sites; need a disposable drive sandbox"},
		{ID: "channel.send", Class: "blocked", Reason: "would send an external message"},
	}
}
