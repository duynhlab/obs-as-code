// Package example holds the worked example every resource file is modelled on.
//
// It is published, unlike the example alert group beside it. An empty output
// tree proves nothing: publishing this board is what makes `make validate`,
// `kustomize build` and a server-side dry-run meaningful, and what lets the
// whole path — Go, resource, OCI artifact, Flux, operator, Grafana — be checked
// end to end before a board anyone depends on travels it. It lands in the
// Examples folder, queries nothing unusual, and pages nobody.
//
// It also gives a new author a file to copy rather than a document to read, and
// keeps the conformance suite exercised: a table-driven suite over an empty
// registry passes for the wrong reason.
package example

import (
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/units"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/panels"
	"github.com/duynhlab/obs-as-code/internal/profile"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

// uid is the Grafana UID, the Kubernetes resource name, and this file's base
// name. Keeping the three the same is how a reader gets from a board in the UI
// to the code that produced it.
const uid = "obs-as-code-example"

func init() {
	registry.Add(registry.Dashboard{
		Meta: registry.Meta{
			UID:     uid,
			Title:   "Example — Golden Signals",
			Folder:  folders.Examples,
			Owner:   "platform",
			Publish: true,
		},
		Build: build,
	})
}

func build(p profile.Profile) *dashboard.DashboardBuilder {
	// $job is a Grafana variable, resolved by the reader's dropdown. It reaches
	// PromQL inside a matcher value, which is the only position the query
	// checks allow — see internal/promql.
	sel := queries.Selector{Job: "$job"}

	return common.NewDashboard(p, uid, "Example — Golden Signals", "example", "golden-signals").
		// A query variable, so the board is one board for every service rather
		// than one board per service.
		WithVariable(dashboard.NewQueryVariableBuilder("job").
			Label("Service").
			Datasource(p.MetricsRef()).
			Query(dashboard.StringOrMap{String: strPtr("label_values(up, job)")}).
			Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
			Multi(true).
			IncludeAll(true).
			Sort(dashboard.VariableSortAlphabeticalAsc)).

		// Rate, errors, duration — the three questions asked first during an
		// incident, so they are the three panels seen without scrolling.
		WithRow(dashboard.NewRowBuilder("Golden signals")).
		WithPanel(panels.Timeseries(p, "Request rate", queries.HTTPRequestRate(sel), "{{http_route}}").
			Description("Requests handled per second, by route.").
			Unit(units.RequestsPerSecond).
			Span(8).Height(8)).
		WithPanel(panels.Timeseries(p, "Error rate", queries.HTTPErrorRate(sel), "{{http_route}}").
			Description("Requests answered with a 5xx, per second. 4xx is the caller's problem and is deliberately absent.").
			Unit(units.RequestsPerSecond).
			Span(8).Height(8)).
		WithPanel(panels.Timeseries(p, "Latency p95", queries.HTTPLatencyQuantile(sel, 0.95), "{{http_route}}").
			Description("95th percentile request duration. `le` stays in the inner aggregation, or this returns NaN.").
			Unit(units.Seconds).
			Span(8).Height(8)).
		WithRow(dashboard.NewRowBuilder("Availability")).
		WithPanel(panels.Stat(p, "Error ratio", queries.HTTPErrorRatio(sel), "").
			Description("Fraction of requests failing. The denominator is guarded with `> 0` so an idle service reports no data rather than NaN.").
			Unit(units.PercentUnit).
			Span(6).Height(6)).
		WithPanel(panels.Stat(p, "Targets up", queries.Up(sel), "{{instance}}").
			Description("Scrape health. A board showing nothing is ambiguous until you know whether the target is even up.").
			Unit(units.Short).
			Span(6).Height(6)).
		WithPanel(panels.Timeseries(p, "Restarts", queries.Restarts(sel), "{{instance}}").
			Description("Process starts over the selected range.").
			Unit(units.Short).
			Span(12).Height(6)).
		WithRow(dashboard.NewRowBuilder("Go runtime")).
		WithPanel(panels.Timeseries(p, "Goroutines", queries.Goroutines(sel), "{{instance}}").
			Unit(units.Short).
			Span(8).Height(7)).
		WithPanel(panels.Timeseries(p, "Heap in use", queries.HeapAllocBytes(sel), "{{instance}}").
			Unit(units.BytesSI).
			Span(8).Height(7)).
		WithPanel(panels.Timeseries(p, "CPU", queries.CPUSeconds(sel), "{{instance}}").
			Description("CPU seconds consumed per second; 1 means one core saturated.").
			Unit(units.Short).
			Span(8).Height(7))
}

func strPtr(s string) *string { return &s }
