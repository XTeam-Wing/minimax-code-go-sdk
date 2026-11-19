package desktop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateCreateGoal(t *testing.T) {
	objective := "reply pong"
	badSend := "later"
	budget := Number(0)
	goodBudget := Number(1000)
	if err := ValidateCreateGoal(CreateGoalReq{SessionID: "s", Objective: &objective, TokenBudget: &goodBudget}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCreateGoal(CreateGoalReq{Objective: &objective}); err == nil {
		t.Fatal("missing session")
	}
	if err := ValidateCreateGoal(CreateGoalReq{SessionID: "s", SendBehavior: &badSend}); err == nil {
		t.Fatal("bad send behavior")
	}
	if err := ValidateCreateGoal(CreateGoalReq{SessionID: "s", TokenBudget: &budget}); err == nil {
		t.Fatal("zero budget")
	}
	if err := ValidateGoalStatus("nope"); err == nil {
		t.Fatal("bad status")
	}
	if err := ValidateGoalStatus(GoalStatusPaused); err != nil {
		t.Fatal(err)
	}
}

func TestGoalStateReadsStrings(t *testing.T) {
	id := mustScalar(t, `"g1"`)
	objective := mustScalar(t, `"ship the note"`)
	status := mustScalar(t, `"active"`)
	session := mustScalar(t, `"s1"`)
	goal := GoalState{GoalID: &id, Objective: &objective, Status: &status, SessionID: &session}
	if got, ok := goal.ID(); !ok || got != "g1" {
		t.Fatal(got, ok)
	}
	if got, ok := goal.ObjectiveText(); !ok || got != "ship the note" {
		t.Fatal(got, ok)
	}
	if got, ok := goal.StatusText(); !ok || got != GoalStatusActive {
		t.Fatal(got, ok)
	}
	if got, ok := goal.SessionText(); !ok || got != "s1" {
		t.Fatal(got, ok)
	}
}

func TestGoalRequestsDoNotInventEffort(t *testing.T) {
	objective := "reply pong"
	behavior := GoalSendQueue
	budget := Number(1000)
	body, err := json.Marshal(CreateGoalReq{
		SessionID:    "s1",
		Objective:    &objective,
		TokenBudget:  &budget,
		SendBehavior: &behavior,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "effort") || strings.Contains(text, "thinking") {
		t.Fatal(text)
	}
	session := "s1"
	effort := "high"
	selected, err := json.Marshal(SelectModelReq{
		ProviderID: "minimax_api",
		ModelID:    "MiniMax-M3.1-Flash",
		SessionID:  &session,
		Thinking:   &ModelThinkingInput{Effort: &effort},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(selected, &got); err != nil {
		t.Fatal(err)
	}
	thinking, _ := got["thinking"].(map[string]any)
	if thinking["effort"] != "high" || got["provider_id"] != "minimax_api" || got["model_id"] != "MiniMax-M3.1-Flash" || got["session_id"] != session {
		t.Fatal(string(selected))
	}
	global, err := json.Marshal(SelectModelReq{
		ProviderID: "minimax_api",
		ModelID:    "MiniMax-M3.1-Flash",
		Thinking:   &ModelThinkingInput{Effort: &effort},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(global), "session_id") {
		t.Fatal(string(global))
	}
}
