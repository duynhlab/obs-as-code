package render_test

import (
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/alerting"
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/expr"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/render"
)

// sampleGroup builds a realistic two-query rule: a Prometheus query in refId A
// and a server-side threshold expression in refId B that the condition names.
func sampleGroup(t *testing.T, mutate func(*alerting.RuleBuilder)) alerting.RuleGroup {
	t.Helper()

	exprUID := "__expr__"

	queryA := alerting.NewQueryBuilder("A").
		DatasourceUid("${ds}").
		RelativeTimeRange(alerting.Duration(600), alerting.Duration(0)).
		Model(prometheus.NewDataqueryBuilder().
			Expr(`sum(rate(http_requests_total[$__rate_interval]))`).
			Instant().
			RefId("A"))

	queryB := alerting.NewQueryBuilder("B").
		DatasourceUid(exprUID).
		Model(expr.NewExprBuilder().TypeThreshold(
			expr.NewTypeThresholdBuilder().
				Conditions([]cog.Builder[expr.ExprTypeThresholdConditions]{
					expr.NewExprTypeThresholdConditionsBuilder().
						Evaluator(expr.NewExprTypeThresholdConditionsEvaluatorBuilder().
							Type(expr.ExprTypeThresholdConditionsEvaluatorTypeGt).
							Params([]float64{0})),
				}).
				Datasource(dashboard.DataSourceRef{Uid: &exprUID, Type: &exprUID}).
				Expression("A").
				RefId("B")))

	rule := alerting.NewRuleBuilder("Requests are flowing").
		Uid("requests-flowing").
		// The SDK validates that a rule names its group, while the CRD does not
		// accept the field at all — the operator sets it from the resource. Both
		// are satisfied by setting it here and dropping it in translation.
		RuleGroup("example-alerts").
		Queries([]cog.Builder[alerting.Query]{queryA, queryB}).
		Condition("B").
		For("5m").
		ExecErrState(alerting.RuleExecErrStateError).
		NoDataState(alerting.RuleNoDataStateNoData).
		Labels(map[string]string{"severity": "warning"}).
		Annotations(map[string]string{
			"summary":     "Request rate dropped to zero",
			"runbook_url": "https://example.invalid/runbook",
		})

	if mutate != nil {
		mutate(rule)
	}

	group, err := alerting.NewRuleGroupBuilder("example-alerts").
		Interval(60).
		Rules([]cog.Builder[alerting.Rule]{rule}).
		Build()
	if err != nil {
		t.Fatalf("RuleGroupBuilder.Build() error = %v", err)
	}
	return group
}

func TestAlertRuleGroupTranslatesToTheCRDShape(t *testing.T) {
	t.Parallel()

	obj, err := render.AlertRuleGroup(profile.Cluster(), render.AlertRuleGroupInput{
		UID:    "example-alerts",
		Folder: folders.Examples,
		Owner:  "platform",
		Group:  sampleGroup(t, nil),
	})
	if err != nil {
		t.Fatalf("AlertRuleGroup() error = %v", err)
	}

	if got, want := obj.Path(), "grafanaalertrulegroup/example-alerts.yaml"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}

	got := decode(t, obj)
	spec, _ := dig(t, got, "spec").(map[string]any)

	// The CRD requires interval as a duration string; the SDK carries seconds.
	if v, want := spec["interval"], "1m0s"; v != want {
		t.Errorf("spec.interval = %v, want %q", v, want)
	}
	if v, want := spec["folderRef"], folders.Examples.UID; v != want {
		t.Errorf("spec.folderRef = %v, want %q", v, want)
	}

	rules, ok := spec["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("spec.rules = %v, want exactly one rule", spec["rules"])
	}
	rule, _ := rules[0].(map[string]any)

	if v, want := rule["uid"], "requests-flowing"; v != want {
		t.Errorf("rule.uid = %v, want %q (the CRD requires it; the SDK leaves it optional)", v, want)
	}
	if v, want := rule["condition"], "B"; v != want {
		t.Errorf("rule.condition = %v, want %q", v, want)
	}
	if v, want := rule["for"], "5m"; v != want {
		t.Errorf("rule.for = %v, want %q", v, want)
	}

	// The SDK writes notification settings under "notification_settings"; the
	// CRD reads "notificationSettings". Sending the SDK's key produces a
	// resource the API server accepts and the operator then mis-syncs.
	if _, wrong := rule["notification_settings"]; wrong {
		t.Error(`rule carries "notification_settings"; the CRD reads "notificationSettings"`)
	}

	// Grafana populates these itself. Sending them can contradict the group the
	// resource declares.
	for _, forbidden := range []string{"folderUID", "ruleGroup", "orgID", "provenance", "updated", "id"} {
		if _, present := rule[forbidden]; present {
			t.Errorf("rule carries %q, which Grafana owns and the CRD does not accept", forbidden)
		}
	}

	data, ok := rule["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("rule.data = %v, want two queries", rule["data"])
	}
	queryA, _ := data[0].(map[string]any)
	if v, want := queryA["refId"], "A"; v != want {
		t.Errorf("data[0].refId = %v, want %q", v, want)
	}
	// The datasource must still be the profile's variable reference, never a
	// literal UID, so an alert queries the same backend as the board beside it.
	if v, want := queryA["datasourceUid"], "${ds}"; v != want {
		t.Errorf("data[0].datasourceUid = %v, want %q", v, want)
	}
	if _, present := queryA["model"]; !present {
		t.Error("data[0].model is absent; the query body did not survive translation")
	}
	if v := dig(t, queryA, "relativeTimeRange", "from"); v != float64(600) {
		t.Errorf("data[0].relativeTimeRange.from = %v, want 600", v)
	}

	queryB, _ := data[1].(map[string]any)
	if v, want := queryB["datasourceUid"], "__expr__"; v != want {
		t.Errorf("data[1].datasourceUid = %v, want %q", v, want)
	}
}

func TestAlertRuleGroupConvertsKeepFiringForToADurationString(t *testing.T) {
	t.Parallel()

	// SDK: int64 nanoseconds. CRD: a duration string. Passing the integer
	// through would be rejected by the CRD's own pattern.
	group := sampleGroup(t, func(r *alerting.RuleBuilder) {
		r.KeepFiringFor(int64(90 * 1000 * 1000 * 1000))
	})

	obj, err := render.AlertRuleGroup(profile.Cluster(), render.AlertRuleGroupInput{
		UID: "keep-firing", Folder: folders.Examples, Group: group,
	})
	if err != nil {
		t.Fatalf("AlertRuleGroup() error = %v", err)
	}

	got := decode(t, obj)
	rules, _ := dig(t, got, "spec", "rules").([]any)
	rule, _ := rules[0].(map[string]any)

	if v, want := rule["keepFiringFor"], "1m30s"; v != want {
		t.Errorf("rule.keepFiringFor = %v, want %q", v, want)
	}
}

func TestAlertRuleGroupRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		build   func(t *testing.T) render.AlertRuleGroupInput
		wantErr string
	}{
		{
			name: "no rules",
			build: func(t *testing.T) render.AlertRuleGroupInput {
				interval := alerting.Duration(60)
				return render.AlertRuleGroupInput{
					UID: "empty", Folder: folders.Examples,
					Group: alerting.RuleGroup{Interval: &interval},
				}
			},
			wantErr: "requires at least one",
		},
		{
			name: "no interval",
			build: func(t *testing.T) render.AlertRuleGroupInput {
				g := sampleGroup(t, nil)
				g.Interval = nil
				return render.AlertRuleGroupInput{UID: "no-interval", Folder: folders.Examples, Group: g}
			},
			wantErr: "interval is required",
		},
		{
			name: "rule without uid",
			build: func(t *testing.T) render.AlertRuleGroupInput {
				g := sampleGroup(t, nil)
				g.Rules[0].Uid = nil
				return render.AlertRuleGroupInput{UID: "no-rule-uid", Folder: folders.Examples, Group: g}
			},
			wantErr: "uid is required by the CRD",
		},
		{
			name: "condition names no query",
			build: func(t *testing.T) render.AlertRuleGroupInput {
				g := sampleGroup(t, nil)
				g.Rules[0].Condition = "Z"
				return render.AlertRuleGroupInput{UID: "dangling", Folder: folders.Examples, Group: g}
			},
			wantErr: `condition "Z" names no query`,
		},
		{
			name: "group uid not a dns label",
			build: func(t *testing.T) render.AlertRuleGroupInput {
				return render.AlertRuleGroupInput{UID: "Bad_Group", Folder: folders.Examples, Group: sampleGroup(t, nil)}
			},
			wantErr: "DNS-1123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := render.AlertRuleGroup(profile.Cluster(), tt.build(t))
			if err == nil {
				t.Fatalf("AlertRuleGroup() = nil error, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("AlertRuleGroup() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
