package appserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func TestGoalWaitAllowsVerifierAfterParentPlateau(t *testing.T) {
	start := time.Date(2026, 11, 19, 22, 0, 0, 0, time.UTC)
	var wait goalWait
	wait.Started = start
	if stop := wait.step(start.Add(20*time.Second), 122178, 6, desktop.GoalStatusActive, ""); stop != "" {
		t.Fatal(stop)
	}
	if stop := wait.step(start.Add(34*time.Second), 147804, 6, desktop.GoalStatusActive, ""); stop != "" {
		t.Fatalf("verifier still inside 90s and under 230000: %s", stop)
	}
	if wait.VerifierSince.IsZero() {
		t.Fatal("verifier clock did not start")
	}
	if stop := wait.step(start.Add(34*time.Second), 230000, 6, desktop.GoalStatusActive, ""); stop != "usage_guard" {
		t.Fatalf("stop %q", stop)
	}
	wait.VerifierSince = start
	if stop := wait.step(start.Add(goalVerifierWindow), 150000, 6, desktop.GoalStatusActive, ""); stop != "verifier_window" {
		t.Fatalf("stop %q", stop)
	}
	wait.VerifierSince = time.Time{}
	wait.LastRequests = 6
	if stop := wait.step(start.Add(3*time.Minute), 150000, 7, desktop.GoalStatusActive, ""); stop != "wall" {
		t.Fatalf("stop %q", stop)
	}
	if stop := wait.step(start.Add(time.Minute), 200000, 8, desktop.GoalStatusActive, "met"); stop != "" {
		t.Fatalf("verdict should end the wait: %s", stop)
	}
}

func TestFeatureReportStaysOffTheAPIInventory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feature.json")
	t.Setenv("MCODE_FEATURE_REPORT", path)
	writeFeatureRow(t, featureRow{
		ID:       "feature.session-model-readback",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "unit",
	})
	writeFeatureRow(t, featureRow{
		ID:       "feature.session-reconnect",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "unit-2",
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Layer    string       `json:"layer"`
		Baseline *int         `json:"baseline_rows"`
		Rows     []featureRow `json:"rows"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Layer != "feature" || doc.Baseline != nil || len(doc.Rows) != 2 || doc.Rows[0].ID != "feature.session-model-readback" || doc.Rows[1].ID != "feature.session-reconnect" {
		t.Fatalf("%s", body)
	}
	if _, ok := coverageIDSet()["feature.session-model-readback"]; ok {
		t.Fatal("feature row is inside the 497 inventory")
	}
}

func coverageIDSet() map[string]bool {
	out := map[string]bool{}
	for _, row := range coverageInventory() {
		out[row.ID] = true
	}
	return out
}
