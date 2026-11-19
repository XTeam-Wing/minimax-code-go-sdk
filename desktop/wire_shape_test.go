package desktop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateSkillWireMatchesReviewer(t *testing.T) {
	agent := "mavis"
	body, err := json.Marshal(CreateSkillReq{
		Name:        "helloskill",
		Description: "hello",
		Content:     "hello",
		AgentName:   &agent,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "description", "content", "agent_name"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %s in %s", key, body)
		}
	}
	if got["name"] != "helloskill" || got["description"] != "hello" || got["content"] != "hello" {
		t.Fatalf("wire %s", body)
	}
}

func TestCreateCronWireIncludesReviewedFields(t *testing.T) {
	enabled := false
	kind := CronScheduleOnce
	mode := CronSessionTargetNew
	when := Number(1893456000000)
	body, err := json.Marshal(CreateCronDefinitionReq{
		Name:          "hello note",
		AgentName:     "mavis",
		Prompt:        "hello",
		Enabled:       &enabled,
		Schedule:      &CronSchedule{Kind: &kind, RunAtMs: &when},
		SessionTarget: &CronSessionTarget{Mode: &mode},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"name":"hello note"`) || !strings.Contains(text, `"prompt":"hello"`) || !strings.Contains(text, `"enabled":false`) {
		t.Fatal(text)
	}
}
