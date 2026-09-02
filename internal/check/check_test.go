package check_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/check"
)

func query(expr, refID, datasource string) map[string]any {
	return map[string]any{"spec": map[string]any{
		"refId": refID,
		"query": map[string]any{
			"group": "prometheus", "datasource": map[string]any{"name": datasource},
			"spec": map[string]any{"expr": expr},
		},
	}}
}

func goodPanel() map[string]any {
	return map[string]any{"kind": "Panel", "spec": map[string]any{
		"title": "Request rate",
		"data": map[string]any{"spec": map[string]any{"queries": []map[string]any{
			query(`sum(rate(http_requests_total{job=~"$job"}[$__rate_interval])) by (route)`, "A", "${ds}"),
		}}},
	}}
}

func boardJSON(t *testing.T, withVariable bool, panels map[string]map[string]any, items []map[string]any) []byte {
	t.Helper()
	variables := []map[string]any{}
	if withVariable {
		variables = append(variables, map[string]any{"kind": "DatasourceVariable", "spec": map[string]any{"name": "ds", "pluginId": "prometheus"}})
	}
	resource := map[string]any{
		"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard",
		"spec": map[string]any{
			"variables": variables, "elements": panels,
			"layout": map[string]any{"kind": "GridLayout", "spec": map[string]any{"items": items}},
		},
	}
	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func item(name string, x, y, width, height int) map[string]any {
	return map[string]any{"spec": map[string]any{
		"x": x, "y": y, "width": width, "height": height,
		"element": map[string]any{"name": name},
	}}
}

func oneBoard(t *testing.T, panel map[string]any) []byte {
	return boardJSON(t, true, map[string]map[string]any{"requests": panel}, []map[string]any{item("requests", 0, 0, 12, 8)})
}

func panelQueries(t *testing.T, panel map[string]any) []map[string]any {
	t.Helper()
	dataSpec := panelDataSpec(t, panel)
	queries, ok := dataSpec["queries"].([]map[string]any)
	if !ok {
		t.Fatalf("panel queries has type %T", dataSpec["queries"])
	}
	return queries
}

func panelSpec(t *testing.T, panel map[string]any) map[string]any {
	t.Helper()
	spec, ok := panel["spec"].(map[string]any)
	if !ok {
		t.Fatalf("panel spec has type %T", panel["spec"])
	}
	return spec
}

func panelDataSpec(t *testing.T, panel map[string]any) map[string]any {
	t.Helper()
	spec := panelSpec(t, panel)
	data, ok := spec["data"].(map[string]any)
	if !ok {
		t.Fatalf("panel data has type %T", spec["data"])
	}
	dataSpec, ok := data["spec"].(map[string]any)
	if !ok {
		t.Fatalf("panel data spec has type %T", data["spec"])
	}
	return dataSpec
}

func firstQuery(t *testing.T, panel map[string]any) map[string]any {
	t.Helper()
	targets := panelQueries(t, panel)
	if len(targets) == 0 {
		t.Fatal("panel has no queries")
	}
	targetSpec, ok := targets[0]["spec"].(map[string]any)
	if !ok {
		t.Fatalf("target spec has type %T", targets[0]["spec"])
	}
	query, ok := targetSpec["query"].(map[string]any)
	if !ok {
		t.Fatalf("query has type %T", targetSpec["query"])
	}
	return query
}

func setExpr(t *testing.T, panel map[string]any, expr string) {
	t.Helper()
	query := firstQuery(t, panel)
	spec, ok := query["spec"].(map[string]any)
	if !ok {
		t.Fatalf("query spec has type %T", query["spec"])
	}
	spec["expr"] = expr
}

func hasRule(violations []check.Violation, rule string) bool {
	for _, violation := range violations {
		if violation.Rule == rule {
			return true
		}
	}
	return false
}

func TestDashboardAcceptsAConformingV2Resource(t *testing.T) {
	t.Parallel()
	if got := check.Dashboard("test", oneBoard(t, goodPanel())); len(got) != 0 {
		t.Fatalf("Dashboard() violations:\n%s", check.Format(got))
	}
}

func TestEveryRuleFires(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rule string
		body func(*testing.T) []byte
	}{
		{"panel without query", check.RulePanelNoTarget, func(t *testing.T) []byte {
			panel := goodPanel()
			delete(panelDataSpec(t, panel), "queries")
			return oneBoard(t, panel)
		}},
		{"panel without title", check.RulePanelNoTitle, func(t *testing.T) []byte {
			panel := goodPanel()
			panelSpec(t, panel)["title"] = ""
			return oneBoard(t, panel)
		}},
		{"overlapping grid items", check.RuleGridOverlap, func(t *testing.T) []byte {
			return boardJSON(t, true, map[string]map[string]any{"a": goodPanel(), "b": goodPanel()}, []map[string]any{item("a", 0, 0, 12, 8), item("b", 6, 0, 12, 8)})
		}},
		{"unreferenced panel", check.RuleGridOverlap, func(t *testing.T) []byte {
			return boardJSON(t, true, map[string]map[string]any{"a": goodPanel()}, nil)
		}},
		{"duplicate refId", check.RuleDuplicateRefID, func(t *testing.T) []byte {
			panel := goodPanel()
			queries := panelQueries(t, panel)
			panelDataSpec(t, panel)["queries"] = append(queries, query("up", "A", "${ds}"))
			return oneBoard(t, panel)
		}},
		{"hardcoded datasource", check.RuleDatasourceRef, func(t *testing.T) []byte {
			panel := goodPanel()
			firstQuery(t, panel)["datasource"] = map[string]any{"name": "prom-main"}
			return oneBoard(t, panel)
		}},
		{"no datasource variable", check.RuleNoDatasourceVar, func(t *testing.T) []byte {
			return boardJSON(t, false, map[string]map[string]any{"requests": goodPanel()}, []map[string]any{item("requests", 0, 0, 12, 8)})
		}},
		{"broken query", check.RuleQuerySyntax, func(t *testing.T) []byte {
			panel := goodPanel()
			setExpr(t, panel, `sum(rate(up[5m])`)
			return oneBoard(t, panel)
		}},
		{"fixed rate window", check.RuleRateInterval, func(t *testing.T) []byte {
			panel := goodPanel()
			setExpr(t, panel, `rate(up[5m])`)
			return oneBoard(t, panel)
		}},
		{"forbidden label", check.RuleForbiddenLabel, func(t *testing.T) []byte {
			panel := goodPanel()
			setExpr(t, panel, `sum(up) by (user_id)`)
			return oneBoard(t, panel)
		}},
		{"database namespace", check.RuleDBNamespace, func(t *testing.T) []byte {
			panel := goodPanel()
			setExpr(t, panel, `db_query_duration_seconds_count`)
			return oneBoard(t, panel)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := check.Dashboard("test", test.body(t))
			if !hasRule(got, test.rule) {
				t.Errorf("rule %q did not fire:\n%s", test.rule, check.Format(got))
			}
		})
	}
}

func TestForbiddenLabelIgnoresSubstrings(t *testing.T) {
	t.Parallel()
	panel := goodPanel()
	setExpr(t, panel, `sum(pipeline_descriptions_total) by (recipe)`)
	if got := check.Dashboard("test", oneBoard(t, panel)); hasRule(got, check.RuleForbiddenLabel) {
		t.Errorf("forbidden-label fired on substring:\n%s", check.Format(got))
	}
}

func TestDashboardRejectsClassicAndInvalidJSON(t *testing.T) {
	t.Parallel()
	for _, body := range [][]byte{[]byte(`{"uid":"classic"}`), []byte(`{"spec":`)} {
		if got := check.Dashboard("bad", body); !hasRule(got, check.RuleQuerySyntax) {
			t.Errorf("Dashboard(%s) did not reject invalid resource", body)
		}
	}
}

func TestFormatSortsAndJoins(t *testing.T) {
	t.Parallel()
	if got := check.Format(nil); got != "" {
		t.Errorf("Format(nil) = %q", got)
	}
	got := check.Format([]check.Violation{{Resource: "z", Rule: "b", Detail: "second"}, {Resource: "a", Rule: "a", Detail: "first"}})
	if lines := strings.Split(got, "\n"); len(lines) != 2 || !strings.HasPrefix(lines[0], "a:") {
		t.Errorf("Format() = %q, want sorted output", got)
	}
}
