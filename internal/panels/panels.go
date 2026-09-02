// Package panels provides the small set of V2 panel factories used by boards.
package panels

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
	sdkcommon "github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"
	"github.com/grafana/grafana-foundation-sdk/go/gauge"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
	"github.com/grafana/grafana-foundation-sdk/go/stat"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"

	"github.com/duynhlab/obs-as-code/internal/profile"
)

type kind uint8

const (
	kindTimeseries kind = iota
	kindStat
	kindGauge
)

// Panel carries one visualization plus its queries and layout dimensions. V2
// stores layout separately from panel elements, so keeping them together here
// prevents a caller from adding an element without a layout reference.
type Panel struct {
	kind        kind
	title       string
	description string
	width       int64
	height      int64
	targets     []cog.Builder[dashboardv2.PanelQueryKind]
	unit        string
	min         *float64
	max         *float64
	thresholds  cog.Builder[dashboardv2.ThresholdsConfig]
	stacking    cog.Builder[sdkcommon.StackingConfig]
	overrides   []override
}

type override struct {
	matcher    dashboardv2.MatcherConfig
	properties []dashboardv2.DynamicConfigValue
}

// Target builds a Prometheus V2 query against the profile's datasource variable.
func Target(p profile.Profile, expr, legend string) *dashboardv2.TargetBuilder {
	query := prometheus.NewQueryV2Builder().
		Datasource(p.MetricsRef()).
		Expr(expr).
		LegendFormat(legend).
		Range(true)
	return dashboardv2.NewTargetBuilder().Query(query)
}

// Timeseries returns a time-series panel with one query.
func Timeseries(p profile.Profile, title, expr, legend string) *Panel {
	return &Panel{kind: kindTimeseries, title: title, width: 12, height: 8, targets: []cog.Builder[dashboardv2.PanelQueryKind]{Target(p, expr, legend)}}
}

// Stat returns a single-value panel showing the latest non-null value.
func Stat(p profile.Profile, title, expr, legend string) *Panel {
	return &Panel{kind: kindStat, title: title, width: 6, height: 4, targets: []cog.Builder[dashboardv2.PanelQueryKind]{Target(p, expr, legend)}}
}

// Gauge returns a gauge panel showing the latest non-null value.
func Gauge(p profile.Profile, title, expr, legend string) *Panel {
	return &Panel{kind: kindGauge, title: title, width: 6, height: 4, targets: []cog.Builder[dashboardv2.PanelQueryKind]{Target(p, expr, legend)}}
}

// Description sets the panel description.
func (p *Panel) Description(value string) *Panel { p.description = value; return p }

// Unit sets the Grafana field unit.
func (p *Panel) Unit(value string) *Panel { p.unit = value; return p }

// Min sets the field minimum.
func (p *Panel) Min(value float64) *Panel { p.min = &value; return p }

// Max sets the field maximum.
func (p *Panel) Max(value float64) *Panel { p.max = &value; return p }

// Thresholds sets the field thresholds.
func (p *Panel) Thresholds(value cog.Builder[dashboardv2.ThresholdsConfig]) *Panel {
	p.thresholds = value
	return p
}

// Stacking configures time-series stacking.
func (p *Panel) Stacking(value cog.Builder[sdkcommon.StackingConfig]) *Panel {
	p.stacking = value
	return p
}

// WithTarget appends another query to the panel.
func (p *Panel) WithTarget(value cog.Builder[dashboardv2.PanelQueryKind]) *Panel {
	p.targets = append(p.targets, value)
	return p
}

// WithOverride appends a field override.
func (p *Panel) WithOverride(matcher dashboardv2.MatcherConfig, properties []dashboardv2.DynamicConfigValue) *Panel {
	p.overrides = append(p.overrides, override{matcher: matcher, properties: properties})
	return p
}

// Span sets the width in Grafana's 24-column grid.
func (p *Panel) Span(value int64) *Panel { p.width = value; return p }

// Height sets the grid height.
func (p *Panel) Height(value int64) *Panel { p.height = value; return p }

// Width returns the configured grid width.
func (p *Panel) Width() int64 { return p.width }

// GridHeight returns the configured grid height.
func (p *Panel) GridHeight() int64 { return p.height }

// Build creates the SDK panel element. The dashboard composer owns IDs because
// it also owns deterministic element ordering.
func (p *Panel) Build(id float64) *dashboardv2.PanelBuilder {
	data := dashboardv2.NewQueryGroupBuilder().Targets(p.targets)
	b := dashboardv2.NewPanelBuilder().Id(id).Title(p.title).Data(data)
	if p.description != "" {
		b = b.Description(p.description)
	}

	switch p.kind {
	case kindStat:
		viz := stat.NewVisualizationV2Builder().ReduceOptions(reduceOptions())
		applyStat(viz, p)
		b = b.Visualization(viz)
	case kindGauge:
		viz := gauge.NewVisualizationV2Builder().
			ShowThresholdMarkers(true).
			ShowThresholdLabels(false).
			ReduceOptions(reduceOptions())
		applyGauge(viz, p)
		b = b.Visualization(viz)
	default:
		viz := timeseries.NewVisualizationV2Builder().
			LineWidth(1).
			FillOpacity(8).
			GradientMode(sdkcommon.GraphGradientModeNone).
			ShowPoints(sdkcommon.VisibilityModeNever).
			Legend(sdkcommon.NewVizLegendOptionsBuilder().
				DisplayMode(sdkcommon.LegendDisplayModeList).
				Placement(sdkcommon.LegendPlacementBottom).
				ShowLegend(true)).
			Tooltip(sdkcommon.NewVizTooltipOptionsBuilder().
				Mode(sdkcommon.TooltipDisplayModeMulti).
				Sort(sdkcommon.SortOrderDescending))
		applyTimeseries(viz, p)
		b = b.Visualization(viz)
	}
	return b
}

func reduceOptions() *sdkcommon.ReduceDataOptionsBuilder {
	return sdkcommon.NewReduceDataOptionsBuilder().Calcs([]string{"lastNotNull"}).Values(false)
}

func applyTimeseries(v *timeseries.VisualizationV2Builder, p *Panel) {
	if p.unit != "" {
		v.Unit(p.unit)
	}
	if p.min != nil {
		v.Min(*p.min)
	}
	if p.max != nil {
		v.Max(*p.max)
	}
	if p.thresholds != nil {
		v.Thresholds(p.thresholds)
	}
	if p.stacking != nil {
		v.Stacking(p.stacking)
	}
	for _, o := range p.overrides {
		v.Override(o.matcher, o.properties)
	}
}

func applyStat(v *stat.VisualizationV2Builder, p *Panel) {
	if p.unit != "" {
		v.Unit(p.unit)
	}
	if p.min != nil {
		v.Min(*p.min)
	}
	if p.max != nil {
		v.Max(*p.max)
	}
	if p.thresholds != nil {
		v.Thresholds(p.thresholds)
	}
	for _, o := range p.overrides {
		v.Override(o.matcher, o.properties)
	}
}

func applyGauge(v *gauge.VisualizationV2Builder, p *Panel) {
	if p.unit != "" {
		v.Unit(p.unit)
	}
	if p.min != nil {
		v.Min(*p.min)
	}
	if p.max != nil {
		v.Max(*p.max)
	}
	if p.thresholds != nil {
		v.Thresholds(p.thresholds)
	}
	for _, o := range p.overrides {
		v.Override(o.matcher, o.properties)
	}
}

// Thresholds builds an absolute threshold set.
func Thresholds(base string, steps ...ThresholdStep) *dashboardv2.ThresholdsConfigBuilder {
	out := []dashboardv2.Threshold{{Color: base, Value: nil}}
	for _, step := range steps {
		value := step.At
		out = append(out, dashboardv2.Threshold{Color: step.Color, Value: &value})
	}
	return dashboardv2.NewThresholdsConfigBuilder().Mode(dashboardv2.ThresholdsModeAbsolute).Steps(out)
}

// ThresholdStep is one colour transition at an absolute value.
type ThresholdStep struct {
	At    float64
	Color string
}
