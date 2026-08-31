package check_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/check"
)

// boardJSON builds a dashboard model with the given panels and, unless
// suppressed, a datasource variable — so each test isolates one rule.
func boardJSON(t *testing.T, withVariable bool, panels ...map[string]any) []byte {
	t.Helper()

	model := map[string]any{
		"uid":    "test-board",
		"title":  "Test Board",
		"panels": panels,
	}
	if withVariable {
		model["templating"] = map[string]any{
			"list": []map[string]any{{"name": "ds", "type": "datasource"}},
		}
	}

	out, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// goodPanel is a panel that breaks no rule; tests mutate a copy of it.
func goodPanel() map[string]any {
	return map[string]any{
		"type":    "timeseries",
		"title":   "Request rate",
		"gridPos": map[string]int{"x": 0, "y": 0, "w": 8, "h": 8},
		"targets": []map[string]any{{
			"refId":      "A",
			"expr":       `sum(rate(http_server_request_duration_seconds_count{job=~"$job"}[$__rate_interval])) by (http_route)`,
			"datasource": map[string]string{"type": "prometheus", "uid": "${ds}"},
		}},
	}
}

// targetsOf returns a panel's targets, failing the test rather than panicking
// if the fixture is malformed.
func targetsOf(t *testing.T, panel map[string]any) []map[string]any {
	t.Helper()

	targets, ok := panel["targets"].([]map[string]any)
	if !ok {
		t.Fatalf("fixture panel has no targets: %v", panel["targets"])
	}
	return targets
}

func hasRule(violations []check.Violation, rule string) bool {
	for _, v := range violations {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

func TestDashboardAcceptsAConformingBoard(t *testing.T) {
	t.Parallel()

	got := check.Dashboard("test-board", boardJSON(t, true, goodPanel()))
	if len(got) != 0 {
		t.Errorf("Dashboard() = %d violations, want none:\n%s", len(got), check.Format(got))
	}
}

// TestEveryRuleFires is the point of this file. A rule that cannot fire is
// worse than no rule: it reports success forever and nobody looks again.
func TestEveryRuleFires(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		rule  string
		model func(t *testing.T) []byte
	}{
		{
			name: "panel with no query",
			rule: check.RulePanelNoTarget,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				delete(p, "targets")
				return boardJSON(t, true, p)
			},
		},
		{
			name: "panel with no title",
			rule: check.RulePanelNoTitle,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				p["title"] = ""
				return boardJSON(t, true, p)
			},
		},
		{
			name: "overlapping panels",
			rule: check.RuleGridOverlap,
			model: func(t *testing.T) []byte {
				a, b := goodPanel(), goodPanel()
				b["title"] = "Error rate"
				// Same cell as a: this is the bug the previous repo shipped.
				b["gridPos"] = map[string]int{"x": 4, "y": 0, "w": 8, "h": 8}
				return boardJSON(t, true, a, b)
			},
		},
		{
			name: "duplicate refId in one panel",
			rule: check.RuleDuplicateRefID,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targets := targetsOf(t, p)
				dup := map[string]any{
					"refId":      "A",
					"expr":       `up{job=~"$job"}`,
					"datasource": map[string]string{"type": "prometheus", "uid": "${ds}"},
				}
				p["targets"] = append(targets, dup)
				return boardJSON(t, true, p)
			},
		},
		{
			name: "hardcoded datasource uid",
			rule: check.RuleDatasourceRef,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["datasource"] =
					map[string]string{"type": "prometheus", "uid": "P1809F7CD0C75ACF3"}
				return boardJSON(t, true, p)
			},
		},
		{
			name: "no datasource variable",
			rule: check.RuleNoDatasourceVar,
			model: func(t *testing.T) []byte {
				return boardJSON(t, false, goodPanel())
			},
		},
		{
			name: "broken query",
			rule: check.RuleQuerySyntax,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["expr"] = `sum(rate(up[5m])`
				return boardJSON(t, true, p)
			},
		},
		{
			name: "metricsql-only query is not portable",
			rule: check.RuleQuerySyntax,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["expr"] = `rate(up{job=~"$job"}[$__rate_interval]) keep_metric_names`
				return boardJSON(t, true, p)
			},
		},
		{
			name: "fixed rate window",
			rule: check.RuleRateInterval,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["expr"] = `sum(rate(up{job=~"$job"}[5m]))`
				return boardJSON(t, true, p)
			},
		},
		{
			name: "forbidden high-cardinality label in a matcher",
			rule: check.RuleForbiddenLabel,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["expr"] = `sum(rate(orders_total{user_id="42"}[$__rate_interval]))`
				return boardJSON(t, true, p)
			},
		},
		{
			name: "forbidden high-cardinality label in a by clause",
			rule: check.RuleForbiddenLabel,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["expr"] = `sum(rate(orders_total{job=~"$job"}[$__rate_interval])) by (order_id)`
				return boardJSON(t, true, p)
			},
		},
		{
			name: "semconv database namespace instead of pgx",
			rule: check.RuleDBNamespace,
			model: func(t *testing.T) []byte {
				p := goodPanel()
				targetsOf(t, p)[0]["expr"] = `sum(rate(db_query_duration_seconds_count{job=~"$job"}[$__rate_interval]))`
				return boardJSON(t, true, p)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := check.Dashboard("test-board", tt.model(t))
			if !hasRule(got, tt.rule) {
				t.Errorf("rule %q did not fire; violations were:\n%s", tt.rule, check.Format(got))
			}
		})
	}
}

func TestDashboardIgnoresRowPanels(t *testing.T) {
	t.Parallel()

	// A row legitimately has no query and no meaningful size; flagging it would
	// make the report noise that people learn to skip.
	row := map[string]any{
		"type":    "row",
		"title":   "Golden signals",
		"gridPos": map[string]int{"x": 0, "y": 0, "w": 24, "h": 1},
	}

	got := check.Dashboard("test-board", boardJSON(t, true, row, goodPanel()))
	if len(got) != 0 {
		t.Errorf("Dashboard() = %d violations, want none:\n%s", len(got), check.Format(got))
	}
}

func TestDashboardChecksPanelsInsideCollapsedRows(t *testing.T) {
	t.Parallel()

	// A collapsed row nests its children, and a board is not exempt from the
	// rules just because a row happened to be saved collapsed.
	broken := goodPanel()
	delete(broken, "targets")

	row := map[string]any{
		"type":    "row",
		"title":   "Collapsed",
		"gridPos": map[string]int{"x": 0, "y": 0, "w": 24, "h": 1},
		"panels":  []map[string]any{broken},
	}

	got := check.Dashboard("test-board", boardJSON(t, true, row))
	if !hasRule(got, check.RulePanelNoTarget) {
		t.Errorf("a broken panel inside a collapsed row was not checked; violations:\n%s", check.Format(got))
	}
}

func TestLabelRuleDoesNotFireOnCoincidentalSubstrings(t *testing.T) {
	t.Parallel()

	// "ip" is forbidden as a label, but a rule that matched the letters would
	// reject every metric containing them and get switched off within a week.
	p := goodPanel()
	targetsOf(t, p)[0]["expr"] =
		`sum(rate(pipeline_descriptions_total{job=~"$job"}[$__rate_interval])) by (recipe)`

	got := check.Dashboard("test-board", boardJSON(t, true, p))
	if hasRule(got, check.RuleForbiddenLabel) {
		t.Errorf("forbidden-label fired on a coincidental substring:\n%s", check.Format(got))
	}
}

func TestDashboardReportsInvalidJSON(t *testing.T) {
	t.Parallel()

	got := check.Dashboard("broken", []byte(`{"uid":`))
	if len(got) == 0 {
		t.Fatal("Dashboard() = no violations for malformed JSON")
	}
}

func TestFormatSortsAndJoins(t *testing.T) {
	t.Parallel()

	if got := check.Format(nil); got != "" {
		t.Errorf("Format(nil) = %q, want empty", got)
	}

	// Sorted, so a CI log diff between two runs is meaningful.
	got := check.Format([]check.Violation{
		{Resource: "z", Rule: "b", Detail: "second"},
		{Resource: "a", Rule: "a", Detail: "first"},
	})
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "a:") {
		t.Errorf("Format() = %q, want sorted output", got)
	}
}
