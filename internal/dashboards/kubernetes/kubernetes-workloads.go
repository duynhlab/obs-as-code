package kubernetes

import (
	sdkcommon "github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"
	"github.com/grafana/grafana-foundation-sdk/go/units"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/panels"
	"github.com/duynhlab/obs-as-code/internal/profile"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

// workloadsUID — filename, Grafana UID and spec.oci path, as always.
const workloadsUID = "kubernetes-workloads"

// workloadsMeta: the drill-down board below the cluster overview.
//
// One deliberate deviation from kubernetes-mixin, which splits namespace,
// workload and pod into three boards: that split exists for thousand-pod
// clusters where each level must stay renderable on its own. At this scale one
// board with chained variables answers the same questions, and because every
// query already takes the selection as a parameter, splitting later changes no
// query — only where it is registered.
var workloadsMeta = registry.Meta{
	UID:     workloadsUID,
	Title:   "Kubernetes Workloads",
	Owner:   "platform",
	Publish: true,
}

func init() {
	registry.Add(registry.Dashboard{Meta: workloadsMeta, Build: buildWorkloads})
}

// wsel carries the board's variables into every query. All three land inside
// matcher values, the only position the query checks allow.
var wsel = queries.WorkloadSelector{
	Namespace:    "$namespace",
	WorkloadType: "$workload_type",
	Workload:     "$workload",
}

func buildWorkloads(p profile.Profile) *dashboard.DashboardBuilder {
	return common.NewDashboard(p, workloadsMeta, "kubernetes", "workloads").
		WithVariable(workloadsNamespaceVariable(p)).
		WithVariable(workloadTypeVariable(p)).
		WithVariable(workloadVariable(p)).
		Link(dashboard.NewDashboardLinkBuilder("Cluster overview").
			Type("link").
			Icon("dashboard").
			Url("/d/kubernetes-cluster-overview")).
		WithRow(dashboard.NewRowBuilder("Workloads in $namespace")).
		WithPanel(wlCPUByWorkload(p)).
		WithPanel(wlMemoryByWorkload(p)).
		WithPanel(wlRestartsByWorkload(p)).
		WithRow(dashboard.NewRowBuilder("CPU")).
		WithPanel(wlCPUByPod(p)).
		WithPanel(wlCPUVsRequests(p)).
		WithPanel(wlThrottling(p)).
		WithRow(dashboard.NewRowBuilder("Memory")).
		WithPanel(wlMemoryByPod(p)).
		WithPanel(wlMemoryVsLimits(p)).
		WithRow(dashboard.NewRowBuilder("Network")).
		WithPanel(wlNetwork(p)).
		WithRow(dashboard.NewRowBuilder("Health")).
		WithPanel(wlRestartsByPod(p)).
		WithPanel(wlWaitingReasons(p))
}

// ---------------------------------------------------------------------------
// Variables — the mixin chain: $namespace → $workload_type → $workload. The
// last two are label_values() over the ownership recording rule, exactly as
// upstream does it; without that rule these dropdowns are empty, which is the
// documented failure signature for the cross-repo dependency.
// ---------------------------------------------------------------------------

func workloadsNamespaceVariable(p profile.Profile) *dashboard.QueryVariableBuilder {
	// Single-select on purpose: this is a drill-down board, and "all
	// namespaces at pod granularity" is the unbounded query the overview
	// exists to avoid.
	return dashboard.NewQueryVariableBuilder("namespace").
		Label("Namespace").
		Datasource(p.MetricsRef()).
		Query(dashboard.StringOrMap{String: strPtr("label_values(kube_pod_info, exported_namespace)")}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		Sort(dashboard.VariableSortAlphabeticalAsc)
}

func workloadTypeVariable(p profile.Profile) *dashboard.QueryVariableBuilder {
	return dashboard.NewQueryVariableBuilder("workload_type").
		Label("Type").
		Datasource(p.MetricsRef()).
		Query(dashboard.StringOrMap{String: strPtr(
			`label_values(` + queries.OwnershipRule + `{namespace=~"$namespace"}, workload_type)`)}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		Multi(true).
		IncludeAll(true).
		Sort(dashboard.VariableSortAlphabeticalAsc)
}

func workloadVariable(p profile.Profile) *dashboard.QueryVariableBuilder {
	return dashboard.NewQueryVariableBuilder("workload").
		Label("Workload").
		Datasource(p.MetricsRef()).
		Query(dashboard.StringOrMap{String: strPtr(
			`label_values(` + queries.OwnershipRule + `{namespace=~"$namespace",workload_type=~"$workload_type"}, workload)`)}).
		Refresh(dashboard.VariableRefreshOnTimeRangeChanged).
		Multi(true).
		IncludeAll(true).
		Sort(dashboard.VariableSortAlphabeticalAsc)
}

// ---------------------------------------------------------------------------
// Workloads
// ---------------------------------------------------------------------------

func wlCPUByWorkload(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "CPU by Workload", queries.CPUByWorkload(wsel), "{{workload_type}}/{{workload}}").
		Description("CPU cores consumed, attributed to the owning Deployment, StatefulSet, DaemonSet or Job through the ownership recording rule.").
		Unit(units.Short).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(8).Height(8)
}

func wlMemoryByWorkload(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Memory by Workload", queries.MemoryByWorkload(wsel), "{{workload_type}}/{{workload}}").
		Description("Working-set memory by workload — the figure the OOM killer acts on.").
		Unit(units.BytesSI).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(8).Height(8)
}

func wlRestartsByWorkload(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Restarts by Workload", queries.RestartsByWorkload(wsel), "{{workload_type}}/{{workload}}").
		Description("Container restarts per second, attributed to the owning workload.").
		Unit(units.Short).
		Span(8).Height(8)
}

// ---------------------------------------------------------------------------
// CPU
// ---------------------------------------------------------------------------

func wlCPUByPod(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "CPU by Pod", queries.CPUByPod(wsel), "{{pod}}").
		Unit(units.Short).
		Span(8).Height(8)
}

func wlCPUVsRequests(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "CPU Usage vs Requests by Pod", queries.CPUVsRequestsByPod(wsel), "{{pod}}").
		Description("Usage as a fraction of requests. Above 1 is legitimate burst; sustained far below 1 is over-provisioning that inflates the scheduling gauges on the overview.").
		Unit(units.PercentUnit).
		Min(0).
		Span(8).Height(8)
}

func wlThrottling(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "CPU Throttling by Pod", queries.ThrottlingByPod(wsel), "{{pod}}").
		Description("Fraction of CFS periods throttled, for the selected workload's pods. Healthy pods stay visible at zero rather than being filtered away.").
		Unit(units.PercentUnit).
		Min(0).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.25, Color: "yellow"},
			panels.ThresholdStep{At: 0.5, Color: "red"})).
		Span(8).Height(8)
}

// ---------------------------------------------------------------------------
// Memory
// ---------------------------------------------------------------------------

func wlMemoryByPod(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Memory by Pod", queries.MemoryByPod(wsel), "{{pod}}").
		Unit(units.BytesSI).
		Span(12).Height(8)
}

func wlMemoryVsLimits(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Memory Usage vs Limits by Pod", queries.MemoryVsLimitsByPod(wsel), "{{pod}}").
		Description("Working set as a fraction of the memory limit — 1.0 is where the OOM killer lives. Same join shape as homelab's KubePodMemoryNearLimit alert, so the panel and the page agree.").
		Unit(units.PercentUnit).
		Min(0).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.8, Color: "yellow"},
			panels.ThresholdStep{At: 0.9, Color: "red"})).
		Span(12).Height(8)
}

// ---------------------------------------------------------------------------
// Network / Health
// ---------------------------------------------------------------------------

func wlNetwork(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Network I/O by Pod", queries.NetworkReceiveByPod(wsel), "RX {{pod}}").
		WithTarget(panels.Target(p, queries.NetworkTransmitByPod(wsel), "TX {{pod}}")).
		Description("Pod network throughput for the selected workload. Transmit is drawn below the axis, matching the overview's convention.").
		Unit(units.BytesPerSecondSI).
		WithOverride(
			dashboard.MatcherConfig{Id: "byRegexp", Options: "TX.*"},
			[]dashboard.DynamicConfigValue{{Id: "custom.transform", Value: "negative-Y"}}).
		Span(24).Height(8)
}

func wlRestartsByPod(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Restarts by Pod", queries.RestartsByPod(wsel), "{{pod}}").
		Unit(units.Short).
		Span(12).Height(8)
}

func wlWaitingReasons(p profile.Profile) *timeseries.PanelBuilder {
	return panels.Timeseries(p, "Containers Waiting by Reason", queries.WaitingReasonsByPod(wsel), "{{pod}} · {{reason}}").
		Description("Containers stuck waiting — CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError. Zero for every pod is the healthy state.").
		Unit(units.Short).
		Span(12).Height(8)
}
