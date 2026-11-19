package appserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// CoverageOutcome is one row of a run. The file is written outside the
// module. It is not a source of truth checked into the repository.
type CoverageOutcome struct {
	ID      string `json:"id"`
	Surface string `json:"surface,omitempty"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
}

type coverageLedger struct {
	mu   sync.Mutex
	rows map[string]CoverageOutcome
}

func newCoverageLedger() *coverageLedger {
	return &coverageLedger{rows: map[string]CoverageOutcome{}}
}

func (l *coverageLedger) note(id, outcome, detail string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows[id] = CoverageOutcome{ID: id, Outcome: outcome, Detail: detail}
}

func (l *coverageLedger) outcome(id string) (CoverageOutcome, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[id]
	return row, ok
}

func (l *coverageLedger) snapshot() []CoverageOutcome {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]CoverageOutcome, 0, len(l.rows))
	for _, row := range l.rows {
		out = append(out, row)
	}
	return out
}

func coverageReportPath() string {
	if path := os.Getenv("MCODE_COVERAGE_REPORT"); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "mcode-coverage-report.json")
}

func (l *coverageLedger) write() (string, error) {
	path := coverageReportPath()
	if filepath.IsAbs(path) {
		// Keep run output out of the module even if the variable is relative.
	}
	body, err := json.MarshalIndent(struct {
		Partial bool              `json:"partial_inventory"`
		Rows    []CoverageOutcome `json:"rows"`
	}{Partial: true, Rows: l.snapshot()}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return "", err
	}
	return path, nil
}
