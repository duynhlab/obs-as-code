// Package example holds the worked example every resource file is modelled on.
//
// It is published as raw Dashboard V2 JSON but deliberately has no Delivery:
// authors can inspect/import it and the conformance suite stays exercised,
// while production receives only the two Kubernetes boards.
//
// It also gives a new author a file to copy rather than a document to read, and
// keeps the conformance suite exercised: a table-driven suite over an empty
// registry passes for the wrong reason.
package example

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/units"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/panels"
	"github.com/duynhlab/obs-as-code/internal/profile"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

// uid is the Grafana UID, the Kubernetes resource name, and this file's base
// name. Keeping the three the same is how a reader gets from a board in the UI
// to the code that produced it.
const uid = "obs-as-code-example"

// meta is declared once and used both to register the board and to build it, so
// the two cannot disagree about what this board is called.
var meta = registry.Meta{
	UID:     uid,
	Title:   "Example — Golden Signals",
	Owner:   "platform",
	Publish: true,
}

// Dashboards returns the example catalog entries.
func Dashboards() []registry.Dashboard {
	return []registry.Dashboard{{
		Meta:  meta,
		Build: func(p profile.Profile) cog.Builder[dashboardv2.Dashboard] { return build(p) },
	}}
}

func build(p profile.Profile) *common.DashboardBuilder {
	// $job is a Grafana variable, resolved by the reader's dropdown. It reaches
	// PromQL inside a matcher value, which is the only position the query
	// checks allow — see internal/promql.
	sel := queries.Selector{Job: "$job"}

	return common.NewDashboard(p, meta, "example", "golden-signals").
		// A query variable, so the board is one board for every service rather
		// than one board per service.
		QueryVariable(common.SelectAll(dashboardv2.NewQueryVariableBuilder("job").
			Label("Service").
			Query(prometheus.NewQueryV2Builder().Datasource(p.MetricsRef()).Expr(queries.JobValues())).
			Refresh(dashboardv2.VariableRefreshOnTimeRangeChanged).
			Multi(true).
			Sort(dashboardv2.VariableSortAlphabeticalAsc))).

		// Rate, errors, duration — the three questions asked first during an
		// incident, so they are the three panels seen without scrolling.
		Row("Golden signals").
		Panel("request-rate", panels.Timeseries(p, "Request rate", queries.HTTPRequestRate(sel), "{{http_route}}").
			Description("Requests handled per second, by route.").
			Unit(units.RequestsPerSecond).
			Span(8).Height(8)).
		Panel("error-rate", panels.Timeseries(p, "Error rate", queries.HTTPErrorRate(sel), "{{http_route}}").
			Description("Requests answered with a 5xx, per second. 4xx is the caller's problem and is deliberately absent.").
			Unit(units.RequestsPerSecond).
			Span(8).Height(8)).
		Panel("latency-p95", panels.Timeseries(p, "Latency p95", queries.HTTPLatencyQuantile(sel, 0.95), "{{http_route}}").
			Description("95th percentile request duration. `le` stays in the inner aggregation, or this returns NaN.").
			Unit(units.Seconds).
			Span(8).Height(8)).
		Row("Availability").
		Panel("error-ratio", panels.Stat(p, "Error ratio", queries.HTTPErrorRatio(sel), "").
			Description("Fraction of requests failing. The denominator is guarded with `> 0` so an idle service reports no data rather than NaN.").
			Unit(units.PercentUnit).
			Span(6).Height(6)).
		Panel("targets-up", panels.Stat(p, "Targets up", queries.Up(sel), "{{instance}}").
			Description("Scrape health. A board showing nothing is ambiguous until you know whether the target is even up.").
			Unit(units.Short).
			Span(6).Height(6)).
		Panel("restarts", panels.Timeseries(p, "Restarts", queries.Restarts(sel), "{{instance}}").
			Description("Process starts over the selected range.").
			Unit(units.Short).
			Span(12).Height(6)).
		Row("Go runtime").
		Panel("goroutines", panels.Timeseries(p, "Goroutines", queries.Goroutines(sel), "{{instance}}").
			Unit(units.Short).
			Span(8).Height(7)).
		Panel("heap-in-use", panels.Timeseries(p, "Heap in use", queries.HeapAllocBytes(sel), "{{instance}}").
			Unit(units.BytesSI).
			Span(8).Height(7)).
		Panel("cpu", panels.Timeseries(p, "CPU", queries.CPUSeconds(sel), "{{instance}}").
			Description("CPU seconds consumed per second; 1 means one core saturated.").
			Unit(units.Short).
			Span(8).Height(7))
}
