// Package panels holds the panel factories boards compose from.
//
// Every factory applies the house defaults and attaches one query, and returns
// the SDK builder so a caller can keep chaining. That is the pattern the
// Foundation SDK's own examples use, and it means this package never has to
// grow an option for every panel field the SDK already exposes.
//
// Layout is always .Span() and .Height(): the SDK computes gridPos from them.
// No caller sets x or y, which is why the overlapping-panel bug the previous
// repo shipped cannot recur here.
package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/gauge"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"

	"github.com/duynhlab/obs-as-code/internal/profile"
)

// Target builds a Prometheus query against the profile's metrics datasource.
//
// The datasource comes from the profile, so no call site names a UID or a
// plugin type — which is what lets one board serve the VictoriaMetrics plugin,
// the local stack, and a plain Prometheus.
func Target(p profile.Profile, expr, legend string) *prometheus.DataqueryBuilder {
	return prometheus.NewDataqueryBuilder().
		Expr(expr).
		LegendFormat(legend).
		Datasource(p.MetricsRef()).
		Range()
}

// Timeseries returns a time series panel with one query and the house defaults.
func Timeseries(p profile.Profile, title, expr, legend string) *timeseries.PanelBuilder {
	return defaultTimeseries(p).
		Title(title).
		WithTarget(Target(p, expr, legend))
}

// Stat returns a single-value panel showing the query's last value.
func Stat(p profile.Profile, title, expr, legend string) *stat.PanelBuilder {
	return stat.NewPanelBuilder().
		Title(title).
		Datasource(p.MetricsRef()).
		Transparent(false).
		ReduceOptions(common.NewReduceDataOptionsBuilder().
			Calcs([]string{"lastNotNull"}).
			Values(false)).
		WithTarget(Target(p, expr, legend))
}

// Gauge returns a single-value panel drawn as a dial against thresholds.
//
// Used where a number only means something relative to a limit — a ratio, a
// saturation level. For a bare count, Stat says the same thing with less ink.
func Gauge(p profile.Profile, title, expr, legend string) *gauge.PanelBuilder {
	return gauge.NewPanelBuilder().
		Title(title).
		Datasource(p.MetricsRef()).
		ShowThresholdMarkers(true).
		ShowThresholdLabels(false).
		ReduceOptions(common.NewReduceDataOptionsBuilder().
			Calcs([]string{"lastNotNull"}).
			Values(false)).
		WithTarget(Target(p, expr, legend))
}

// Thresholds builds an absolute threshold set from a base colour and ordered
// steps.
//
// The base step must carry a nil value, not zero: Grafana serialises the
// -Infinity lower bound as null, and a step at 0 is a different thing that
// renders differently. Every panel has one, so getting it wrong is a whole-board
// mistake.
func Thresholds(base string, steps ...ThresholdStep) *dashboard.ThresholdsConfigBuilder {
	out := make([]dashboard.Threshold, 0, len(steps)+1)
	out = append(out, dashboard.Threshold{Color: base, Value: nil})
	for _, s := range steps {
		value := s.At
		out = append(out, dashboard.Threshold{Color: s.Color, Value: &value})
	}

	return dashboard.NewThresholdsConfigBuilder().
		Mode(dashboard.ThresholdsModeAbsolute).
		Steps(out)
}

// ThresholdStep is one colour change, at an absolute value.
//
// Absolute rather than percentage: percentage mode is relative to the field's
// min and max, so a "70" on a 0..1 ratio means 0.7 — correct, and read as a
// mistake by everyone who sees it.
type ThresholdStep struct {
	At    float64
	Color string
}

// defaultTimeseries is the shared base every time series panel starts from, so
// the boards look like one system rather than like the order they were written
// in.
func defaultTimeseries(p profile.Profile) *timeseries.PanelBuilder {
	return timeseries.NewPanelBuilder().
		Datasource(p.MetricsRef()).
		// Rates and counts cannot be negative, and letting the axis float means
		// a flat line at zero is drawn halfway up the panel.
		Min(0).
		LineWidth(1).
		FillOpacity(8).
		GradientMode(common.GraphGradientModeNone).
		ShowPoints(common.VisibilityModeNever).
		Legend(common.NewVizLegendOptionsBuilder().
			DisplayMode(common.LegendDisplayModeList).
			Placement(common.LegendPlacementBottom).
			ShowLegend(true)).
		Tooltip(common.NewVizTooltipOptionsBuilder().
			Mode(common.TooltipDisplayModeMulti).
			Sort(common.SortOrderDescending))
}
