// Package example holds the worked example every alert file is modelled on.
//
// Registered with Publish false, like the example dashboard: the conformance
// suite renders it on every run, so the SDK-to-CRD translation in
// internal/render is exercised end to end rather than only by its own unit
// tests. Nothing is shipped, and no engine decision is implied — whether
// Grafana-managed alerting replaces the 73 PrometheusRule files vmalert
// evaluates today is a separate decision.
package example

import (
	"github.com/grafana/grafana-foundation-sdk/go/alerting"
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/expr"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/profile"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

const (
	uid = "obs-as-code-example-alerts"

	// exprDatasource is the reserved UID for a Grafana server-side expression.
	exprDatasource = "__expr__"
)

func init() {
	registry.Add(registry.AlertGroup{
		Meta: registry.Meta{
			UID:     uid,
			Title:   "Example — Error ratio",
			Folder:  folders.Examples,
			Owner:   "platform",
			Publish: false,
		},
		Build: build,
	})
}

func build(p profile.Profile) *alerting.RuleGroupBuilder {
	return alerting.NewRuleGroupBuilder(uid).
		// Seconds. Evaluating faster than the scrape interval only costs query
		// load; it cannot make the alert more timely.
		Interval(60).
		Rules([]cog.Builder[alerting.Rule]{errorRatioRule(p)})
}

// errorRatioRule fires when more than 5% of requests fail for five minutes.
//
// The query comes from internal/queries, the same function that feeds the
// example dashboard's error-ratio panel — so the graph an on-call engineer
// opens and the rule that woke them cannot disagree about what "error ratio"
// means. That shared definition is the main reason queries are kept free of any
// Grafana SDK types.
func errorRatioRule(p profile.Profile) *alerting.RuleBuilder {
	sel := queries.Selector{Job: "$job"}

	query := alerting.NewQueryBuilder("A").
		DatasourceUid(p.VarRef(p.MetricsVar)).
		RelativeTimeRange(alerting.Duration(600), alerting.Duration(0)).
		Model(prometheus.NewDataqueryBuilder().
			Expr(queries.HTTPErrorRatio(sel)).
			Instant().
			RefId("A"))

	threshold := alerting.NewQueryBuilder("B").
		DatasourceUid(exprDatasource).
		Model(expr.NewExprBuilder().TypeThreshold(
			expr.NewTypeThresholdBuilder().
				Conditions([]cog.Builder[expr.ExprTypeThresholdConditions]{
					expr.NewExprTypeThresholdConditionsBuilder().
						Evaluator(expr.NewExprTypeThresholdConditionsEvaluatorBuilder().
							Type(expr.ExprTypeThresholdConditionsEvaluatorTypeGt).
							Params([]float64{0.05})),
				}).
				Datasource(dashboard.DataSourceRef{
					Uid:  strPtr(exprDatasource),
					Type: strPtr(exprDatasource),
				}).
				Expression("A").
				RefId("B")))

	return alerting.NewRuleBuilder("Example — error ratio above 5%").
		Uid("obs-as-code-example-error-ratio").
		// Required by the SDK's own validation; dropped in translation because
		// the CRD does not accept it and the operator sets it from the group.
		RuleGroup(uid).
		Queries([]cog.Builder[alerting.Query]{query, threshold}).
		Condition("B").
		For("5m").
		// NoData is not Alerting: a service with no traffic has no error ratio,
		// and paging someone because a queue is quiet trains them to ignore the
		// alert.
		NoDataState(alerting.RuleNoDataStateNoData).
		ExecErrState(alerting.RuleExecErrStateError).
		Labels(map[string]string{
			"severity": "warning",
		}).
		Annotations(map[string]string{
			"summary":     "More than 5% of requests to {{ $labels.job }} are failing",
			"description": "Error ratio has been above 5% for five minutes. Check the Example — Golden Signals board for which route is failing.",
			"runbook_url": "https://github.com/duynhlab/obs-as-code/blob/main/docs/runbooks/error-ratio.md",
		})
}

func strPtr(s string) *string { return &s }
