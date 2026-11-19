package appserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
