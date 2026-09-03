package check_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
		// A valid current is part of the default fixture so every test that
		// builds a board also covers the happy path of RuleDatasourceVarValue.
		// Cases that need a bad one use datasourceVarBoard instead.
		variables = append(variables, map[string]any{"kind": "DatasourceVariable", "spec": map[string]any{
			"name": "ds", "pluginId": "prometheus",
			"current": map[string]any{"text": "VictoriaMetrics (Prometheus)", "value": "victoriametrics-prometheus"},
		}})
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
		if got := check.Dashboard("bad", body); !hasRule(got, check.RuleResourceKind) {
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

// datasourceVarBoard builds a board whose datasource variable carries an
// arbitrary `current`, which the shared boardJSON helper cannot express.
func datasourceVarBoard(t *testing.T, current any) []byte {
	t.Helper()
	spec := map[string]any{"name": "ds", "pluginId": "prometheus"}
	if current != nil {
		spec["current"] = current
	}
	resource := map[string]any{
		"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard",
		"spec": map[string]any{
			"variables": []map[string]any{{"kind": "DatasourceVariable", "spec": spec}},
			"elements":  map[string]map[string]any{"requests": goodPanel()},
			"layout": map[string]any{"kind": "GridLayout", "spec": map[string]any{
				"items": []map[string]any{item("requests", 0, 0, 12, 8)},
			}},
		},
	}
	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDashboardDatasourceVarValue is the regression test for the defect that
// made every V2 board render empty: the datasource variable's current.value
// held the datasource's DISPLAY NAME instead of its UID, so ${ds} expanded to a
// string Grafana could not resolve and no panel issued a query.
//
// Nothing here can prove a uid exists in a real Grafana — that is a cluster
// fact. What it proves is that the value has the shape of a uid, which is the
// half of the contract this repo owns.
func TestDashboardDatasourceVarValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current any
		wantFir bool
	}{
		{
			name:    "display name is not a uid",
			current: map[string]any{"text": "VictoriaMetrics (Prometheus)", "value": "VictoriaMetrics (Prometheus)"},
			wantFir: true,
		},
		{
			name:    "uid",
			current: map[string]any{"text": "VictoriaMetrics (Prometheus)", "value": "victoriametrics-prometheus"},
		},
		{
			// Absent current is allowed on purpose: a board may legitimately
			// defer the choice to Grafana's default datasource.
			name:    "no current",
			current: nil,
		},
		{name: "uppercase", current: map[string]any{"value": "VictoriaMetrics"}, wantFir: true},
		{name: "spaces", current: map[string]any{"value": "my datasource"}, wantFir: true},
		{
			// Worse than absent: an empty string resolves to nothing while
			// looking like a deliberate choice.
			name:    "empty string",
			current: map[string]any{"value": ""},
			wantFir: true,
		},
		{
			// A multi-value datasource variable makes no sense for this repo,
			// but the rule must decide rather than panic on the type.
			name:    "array value",
			current: map[string]any{"value": []string{"victoriametrics-prometheus"}},
			wantFir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := check.Dashboard("board", datasourceVarBoard(t, tt.current))

			var fired bool
			for _, v := range got {
				if v.Rule == check.RuleDatasourceVarValue {
					fired = true
				}
			}
			if fired != tt.wantFir {
				t.Errorf("RuleDatasourceVarValue fired = %v, want %v (violations: %v)", fired, tt.wantFir, got)
			}
		})
	}
}

// queryVarBoard builds a board carrying one QueryVariable with an arbitrary
// current, alongside the valid datasource variable every board needs.
func queryVarBoard(t *testing.T, current any) []byte {
	t.Helper()
	qSpec := map[string]any{
		"name": "namespace", "includeAll": true,
		// A valid query keeps RuleVariableQuery quiet so only the current is
		// on trial here.
		"query": map[string]any{
			"kind": "DataQuery", "group": "prometheus",
			"datasource": map[string]any{"name": "${ds}"},
			"spec":       map[string]any{"expr": "label_values(kube_pod_info, exported_namespace)"},
		},
	}
	if current != nil {
		qSpec["current"] = current
	}
	resource := map[string]any{
		"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard",
		"spec": map[string]any{
			"variables": []map[string]any{
				{"kind": "DatasourceVariable", "spec": map[string]any{
					"name": "ds", "pluginId": "prometheus",
					"current": map[string]any{"text": "VM", "value": "victoriametrics-prometheus"},
				}},
				{"kind": "QueryVariable", "spec": qSpec},
			},
			"elements": map[string]map[string]any{"requests": goodPanel()},
			"layout": map[string]any{"kind": "GridLayout", "spec": map[string]any{
				"items": []map[string]any{item("requests", 0, 0, 12, 8)},
			}},
		},
	}
	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDashboardQueryVarValue is the regression test for the defect that shipped
// three empty boards: V2's QueryVariableSpec.Current has no omitempty, so a
// variable nobody set serialises as {"text":"","value":""} and Grafana honours
// it as a real selection of the empty string. Classic dashboards emitted
// current: null and Grafana resolved it from the options, which is exactly why
// the pre-V2 boards kept working.
//
// Measured on the cluster: namespace=~"" returned 0 data points against 28 for
// namespace=~".*".
func TestDashboardQueryVarValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current any
		wantFir bool
	}{
		{
			name:    "unset current serialises empty",
			current: map[string]any{"text": "", "value": ""},
			wantFir: true,
		},
		{
			name:    "all selected",
			current: map[string]any{"text": "All", "value": "$__all"},
		},
		{
			name:    "one concrete value",
			current: map[string]any{"text": "monitoring", "value": "monitoring"},
		},
		{
			// Multi-select stores an array, which must not be read as empty.
			name:    "array of values",
			current: map[string]any{"text": []string{"a", "b"}, "value": []string{"a", "b"}},
		},
		{
			name:    "empty array selects nothing",
			current: map[string]any{"text": []string{}, "value": []string{}},
			wantFir: true,
		},
		{
			// A missing current cannot happen through the SDK, but a
			// hand-edited artifact could carry one and it is still broken.
			name:    "no current at all",
			current: nil,
			wantFir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := check.Dashboard("board", queryVarBoard(t, tt.current))

			var fired bool
			for _, v := range got {
				if v.Rule == check.RuleQueryVarValue {
					fired = true
				}
			}
			if fired != tt.wantFir {
				t.Errorf("RuleQueryVarValue fired = %v, want %v (violations: %v)", fired, tt.wantFir, got)
			}
		})
	}
}

// varQueryBoard builds a board whose one QueryVariable carries expr, with a
// valid current so RuleQueryVarValue stays quiet and only the query is on trial.
func varQueryBoard(t *testing.T, expr string) []byte {
	t.Helper()
	qSpec := map[string]any{
		"name":       "namespace",
		"includeAll": true,
		"current":    map[string]any{"text": "All", "value": "$__all"},
		"query": map[string]any{
			"kind": "DataQuery", "group": "prometheus",
			"datasource": map[string]any{"name": "${ds}"},
			"spec":       map[string]any{"expr": expr},
		},
	}
	resource := map[string]any{
		"apiVersion": "dashboard.grafana.app/v2", "kind": "Dashboard",
		"spec": map[string]any{
			"variables": []map[string]any{
				{"kind": "DatasourceVariable", "spec": map[string]any{
					"name": "ds", "pluginId": "prometheus",
					"current": map[string]any{"text": "VM", "value": "victoriametrics-prometheus"},
				}},
				{"kind": "QueryVariable", "spec": qSpec},
			},
			"elements": map[string]map[string]any{"requests": goodPanel()},
			"layout": map[string]any{"kind": "GridLayout", "spec": map[string]any{
				"items": []map[string]any{item("requests", 0, 0, 12, 8)},
			}},
		},
	}
	out, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestDashboardVariableQuery covers the hole that let both blank-board defects
// through: promql.Check had exactly one non-test call site, inside checkTarget,
// so panel queries were gated and variable queries were not — while both bugs
// that shipped an unusable board lived in variables.
//
// The wrapper is a Grafana datasource function, not PromQL, so pointing the
// gate straight at these expressions rejects the project's own output. What is
// checkable is the series selector inside it.
func TestDashboardVariableQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{
			name: "wrapped selector parses",
			expr: `label_values(kube_pod_info{namespace=~"$namespace"}, exported_namespace)`,
		},
		{
			name: "plain promql parses",
			expr: `up{job="kubelet"}`,
		},
		{
			// The defect this rule exists to catch: a malformed selector
			// yields an empty dropdown, and an empty dropdown means every
			// panel filtering on the variable matches nothing.
			expr: `label_values(kube_pod_info{namespace=~"$namespace", exported_namespace)`,
			name: "wrapped selector is malformed",
			want: true,
		},
		{
			name: "wrapped selector is empty",
			expr: `label_values(, exported_namespace)`,
			want: true,
		},
		{
			name: "no expression at all",
			expr: "",
			want: true,
		},
		{
			// Conventions apply inside the wrapper too. A fixed window in a
			// variable query is as wrong as one in a panel.
			name: "fixed range window inside the wrapper",
			expr: `label_values(rate(kube_pod_info[5m]), exported_namespace)`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := check.Dashboard("board", varQueryBoard(t, tt.expr))

			var fired bool
			for _, v := range got {
				if v.Rule == check.RuleVariableQuery || v.Rule == check.RuleRateInterval {
					fired = true
				}
			}
			if fired != tt.want {
				t.Errorf("variable query %q: fired = %v, want %v (violations: %v)", tt.expr, fired, tt.want, got)
			}
		})
	}
}

// TestPanelEmptyExpressionIsAViolation closes the third way a panel could ship
// empty in silence. checkTarget returned early on a blank expression, so
// promql.Check never ran, and RulePanelNoTarget could not fire either because
// the target existed — it just had nothing in it.
func TestPanelEmptyExpressionIsAViolation(t *testing.T) {
	t.Parallel()

	for _, expr := range []string{"", "   "} {
		panel := map[string]any{"kind": "Panel", "spec": map[string]any{
			"title": "Request rate",
			"data": map[string]any{"spec": map[string]any{"queries": []map[string]any{
				query(expr, "A", "${ds}"),
			}}},
		}}

		var fired bool
		got := check.Dashboard("board", oneBoard(t, panel))
		for _, v := range got {
			if v.Rule == check.RulePanelNoTarget {
				fired = true
			}
		}
		if !fired {
			t.Errorf("expr %q: RulePanelNoTarget did not fire (violations: %v)", expr, got)
		}
	}
}

// TestRateWindowRuleSeesThroughNesting is the regression test for a regex that
// stopped at the first closing paren: `[^)]*` between the function name and the
// range window cannot cross a nested call, so rate(label_replace(...)[5m])
// passed the rule it exists to enforce. This repo's own boards nest
// label_replace inside aggregations, so the shape is not hypothetical.
//
// The rule stays scoped to the rate family on purpose. A fixed window on
// max_over_time is a deliberate lookback — "was it crashlooping in the last
// 5m" — where the window is the question being asked, and two panels in
// generated/ ask exactly that. Widening the rule to every duration literal
// would reject the project's own correct output.
func TestRateWindowRuleSeesThroughNesting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{
			name: "plain fixed window",
			expr: `rate(up[5m])`,
			want: true,
		},
		{
			name: "fixed window behind a nested call",
			expr: `rate(label_replace(up, "ns", "$1", "namespace", "(.*)")[5m])`,
			want: true,
		},
		{
			name: "fixed window two calls deep",
			expr: `sum(increase(label_replace(clamp_min(up, 0), "a", "$1", "b", "(.*)")[1h])) by (job)`,
			want: true,
		},
		{
			name: "rate interval behind a nested call",
			expr: `rate(label_replace(up, "ns", "$1", "namespace", "(.*)")[$__rate_interval])`,
		},
		{
			// The two real panels the narrower rule protects.
			name: "max_over_time keeps its deliberate window",
			expr: `count(max_over_time(kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff"}[5m]) == 1)`,
		},
		{
			name: "count_over_time keeps its deliberate window",
			expr: `count_over_time(up[1h])`,
		},
		{
			// A duration inside a label value is not a range window.
			name: "duration-looking label value",
			expr: `sum(rate(alerts_total{window="5m"}[$__rate_interval]))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			panel := goodPanel()
			setExpr(t, panel, tt.expr)
			got := check.Dashboard("board", oneBoard(t, panel))

			if fired := hasRule(got, check.RuleRateInterval); fired != tt.want {
				t.Errorf("RuleRateInterval fired = %v, want %v for %q (violations: %v)", fired, tt.want, tt.expr, got)
			}
		})
	}
}

// TestForbiddenLabelCoversJoinsAndRewrites extends the cardinality rule to the
// two positions where a high-cardinality label is most often dragged in without
// ever appearing in a matcher or a by() clause: a vector-matching clause, and
// the destination of label_replace/label_join.
func TestForbiddenLabelCoversJoinsAndRewrites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{name: "on clause", expr: `up * on(trace_id) group_left() up`, want: true},
		{name: "ignoring clause", expr: `up * ignoring(user_id) up`, want: true},
		{name: "group_left labels", expr: `up * on(job) group_left(order_id) up`, want: true},
		{name: "group_right labels", expr: `up * on(job) group_right(session_id) up`, want: true},
		{
			name: "label_replace destination",
			expr: `label_replace(up, "email", "$1", "instance", "(.*)")`,
			want: true,
		},
		{
			name: "label_join destination",
			expr: `label_join(up, "request_id", "-", "job", "instance")`,
			want: true,
		},
		{
			// The shape this repo actually uses: a benign destination fed by a
			// benign source must stay quiet, or the rule is unusable here.
			name: "the repo's own label_replace",
			expr: `label_replace(sum by (exported_namespace) (kube_pod_info), "namespace", "$1", "exported_namespace", "(.*)")`,
		},
		{name: "benign join", expr: `up * on(job) group_left(cluster) up`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			panel := goodPanel()
			setExpr(t, panel, tt.expr)
			got := check.Dashboard("board", oneBoard(t, panel))

			if fired := hasRule(got, check.RuleForbiddenLabel); fired != tt.want {
				t.Errorf("RuleForbiddenLabel fired = %v, want %v for %q (violations: %v)", fired, tt.want, tt.expr, got)
			}
		})
	}
}

// TestUnknownElementKindIsRejected inverts the element gate. `if kind ==
// "Panel"` silently skipped anything else, so a LibraryPanel or a future kind
// would ship with no title, query or datasource check at all — passing the
// suite by being invisible to it.
func TestUnknownElementKindIsRejected(t *testing.T) {
	t.Parallel()

	panel := goodPanel()
	panel["kind"] = "LibraryPanel"

	got := check.Dashboard("board", oneBoard(t, panel))
	if !hasRule(got, check.RuleElementKind) {
		t.Errorf("RuleElementKind did not fire for a LibraryPanel:\n%s", check.Format(got))
	}
}

// TestResourceKindHasItsOwnRule separates "this file is not a V2 Dashboard"
// from "this PromQL does not parse". One rule name for both sent a reader to
// the wrong file.
func TestResourceKindHasItsOwnRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		model []byte
	}{
		{name: "not JSON", model: []byte("{not json")},
		{name: "classic dashboard", model: []byte(`{"schemaVersion":39,"panels":[]}`)},
		{name: "wrong kind", model: []byte(`{"apiVersion":"dashboard.grafana.app/v2","kind":"Folder"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := check.Dashboard("board", tt.model)
			if !hasRule(got, check.RuleResourceKind) {
				t.Errorf("RuleResourceKind did not fire:\n%s", check.Format(got))
			}
			if hasRule(got, check.RuleQuerySyntax) {
				t.Errorf("RuleQuerySyntax fired for a resource-level problem:\n%s", check.Format(got))
			}
		})
	}
}

// TestEveryRuleHasATest closes the hole in the suite itself. TestEveryRuleFires
// calls itself "every rule" while being a hand-typed list, and nothing made
// that list complete — the same silent-omission shape as a dashboard domain
// missing from the catalog. Adding a rule and forgetting its negative test left
// the suite green and the claim in the test's name false.
//
// AGENTS.md: "Each enforceable rule has a negative test proving it fires."
func TestEveryRuleHasATest(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("check_test.go")
	if err != nil {
		t.Fatalf("read own source: %v", err)
	}
	tests := string(source)

	rules := declaredRules(t)
	if len(rules) == 0 {
		t.Fatal("found no rule constants, so this test proved nothing")
	}

	for _, rule := range rules {
		// The declaration inside this very test does not count as a use.
		uses := strings.Count(tests, "check."+rule)
		if uses == 0 {
			t.Errorf("check.%s is declared but no test asserts it fires; add a case proving it", rule)
		}
	}
}

// declaredRules returns the names of every Rule* constant in check.go.
func declaredRules(t *testing.T) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "check.go", nil, 0)
	if err != nil {
		t.Fatalf("parse check.go: %v", err)
	}

	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if strings.HasPrefix(name.Name, "Rule") {
					out = append(out, name.Name)
				}
			}
		}
	}
	return out
}
