package kubernetes

import (
	sdkcommon "github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"
	"github.com/grafana/grafana-foundation-sdk/go/prometheus"
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

// wsel carries the board's variables into every query. All three land inside
// matcher values, the only position the query checks allow.
var wsel = queries.WorkloadSelector{
	Namespace:    "$namespace",
	WorkloadType: "$workload_type",
	Workload:     "$workload",
}

func buildWorkloads(p profile.Profile) *common.DashboardBuilder {
	return common.NewDashboard(p, workloadsMeta, "kubernetes", "workloads").
		QueryVariable(workloadsNamespaceVariable(p)).
		QueryVariable(workloadTypeVariable(p)).
		QueryVariable(workloadVariable(p)).
		Link(dashboardv2.NewDashboardLinkBuilder().Title("Cluster overview").
			Type(dashboardv2.DashboardLinkTypeLink).
			Icon("dashboard").
			Url("/d/kubernetes-cluster-overview")).
		Row("Workloads in $namespace").
		Panel("cpu-by-workload", wlCPUByWorkload(p)).
		Panel("memory-by-workload", wlMemoryByWorkload(p)).
		Panel("restarts-by-workload", wlRestartsByWorkload(p)).
		Row("CPU").
		Panel("cpu-by-pod", wlCPUByPod(p)).
		Panel("cpu-vs-requests", wlCPUVsRequests(p)).
		Panel("cpu-throttling", wlThrottling(p)).
		Row("Memory").
		Panel("memory-by-pod", wlMemoryByPod(p)).
		Panel("memory-vs-limits", wlMemoryVsLimits(p)).
		Row("Network").
		Panel("network-io", wlNetwork(p)).
		Row("Health").
		Panel("restarts-by-pod", wlRestartsByPod(p)).
		Panel("waiting-reasons", wlWaitingReasons(p))
}

// ---------------------------------------------------------------------------
// Variables — the mixin chain: $namespace → $workload_type → $workload. The
// last two are label_values() over the ownership recording rule, exactly as
// upstream does it; without that rule these dropdowns are empty, which is the
// documented failure signature for the cross-repo dependency.
// ---------------------------------------------------------------------------

func workloadsNamespaceVariable(p profile.Profile) *dashboardv2.QueryVariableBuilder {
	// Was single-select, because "all namespaces at pod granularity" is the
	// unbounded query the overview exists to avoid. That intent cost the board
	// every panel: V2 has no way to say "no selection yet", so a single-select
	// variable with no Current shipped an empty selection and matched nothing.
	// Opening on All and letting the reader narrow is the behaviour the classic
	// board actually had, since Grafana resolved its null current from the
	// options.
	return common.SelectAll(dashboardv2.NewQueryVariableBuilder("namespace").
		Label("Namespace").
		Query(prometheus.NewQueryV2Builder().Datasource(p.MetricsRef()).Expr("label_values(kube_pod_info, exported_namespace)")).
		Refresh(dashboardv2.VariableRefreshOnTimeRangeChanged).
		Multi(true).
		Sort(dashboardv2.VariableSortAlphabeticalAsc))
}

func workloadTypeVariable(p profile.Profile) *dashboardv2.QueryVariableBuilder {
	return common.SelectAll(dashboardv2.NewQueryVariableBuilder("workload_type").
		Label("Type").
		Query(prometheus.NewQueryV2Builder().Datasource(p.MetricsRef()).Expr(
			`label_values(` + queries.OwnershipRule + `{namespace=~"$namespace"}, workload_type)`)).
		Refresh(dashboardv2.VariableRefreshOnTimeRangeChanged).
		Multi(true).
		Sort(dashboardv2.VariableSortAlphabeticalAsc))
}

func workloadVariable(p profile.Profile) *dashboardv2.QueryVariableBuilder {
	return common.SelectAll(dashboardv2.NewQueryVariableBuilder("workload").
		Label("Workload").
		Query(prometheus.NewQueryV2Builder().Datasource(p.MetricsRef()).Expr(
			`label_values(` + queries.OwnershipRule + `{namespace=~"$namespace",workload_type=~"$workload_type"}, workload)`)).
		Refresh(dashboardv2.VariableRefreshOnTimeRangeChanged).
		Multi(true).
		Sort(dashboardv2.VariableSortAlphabeticalAsc))
}

// ---------------------------------------------------------------------------
// Workloads
// ---------------------------------------------------------------------------

func wlCPUByWorkload(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "CPU by Workload", queries.CPUByWorkload(wsel), "{{workload_type}}/{{workload}}").
		Description("CPU cores consumed, attributed to the owning Deployment, StatefulSet, DaemonSet or Job through the ownership recording rule.").
		Unit(units.Short).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(8).Height(8)
}

func wlMemoryByWorkload(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Memory by Workload", queries.MemoryByWorkload(wsel), "{{workload_type}}/{{workload}}").
		Description("Working-set memory by workload — the figure the OOM killer acts on.").
		Unit(units.BytesSI).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(8).Height(8)
}

func wlRestartsByWorkload(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Restarts by Workload", queries.RestartsByWorkload(wsel), "{{workload_type}}/{{workload}}").
		Description("Container restarts per second, attributed to the owning workload.").
		Unit(units.Short).
		Span(8).Height(8)
}

// ---------------------------------------------------------------------------
// CPU
// ---------------------------------------------------------------------------

func wlCPUByPod(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "CPU by Pod", queries.CPUByPod(wsel), "{{pod}}").
		Unit(units.Short).
		Span(8).Height(8)
}

func wlCPUVsRequests(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "CPU Usage vs Requests by Pod", queries.CPUVsRequestsByPod(wsel), "{{pod}}").
		Description("Usage as a fraction of requests. Above 1 is legitimate burst; sustained far below 1 is over-provisioning that inflates the scheduling gauges on the overview.").
		Unit(units.PercentUnit).
		Min(0).
		Span(8).Height(8)
}

func wlThrottling(p profile.Profile) *panels.Panel {
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

func wlMemoryByPod(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Memory by Pod", queries.MemoryByPod(wsel), "{{pod}}").
		Unit(units.BytesSI).
		Span(12).Height(8)
}

func wlMemoryVsLimits(p profile.Profile) *panels.Panel {
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

func wlNetwork(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Network I/O by Pod", queries.NetworkReceiveByPod(wsel), "RX {{pod}}").
		WithTarget(panels.Target(p, queries.NetworkTransmitByPod(wsel), "TX {{pod}}")).
		Description("Pod network throughput for the selected workload. Transmit is drawn below the axis, matching the overview's convention.").
		Unit(units.BytesPerSecondSI).
		WithOverride(
			dashboardv2.MatcherConfig{Id: "byRegexp", Options: "TX.*"},
			[]dashboardv2.DynamicConfigValue{{Id: "custom.transform", Value: "negative-Y"}}).
		Span(24).Height(8)
}

func wlRestartsByPod(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Restarts by Pod", queries.RestartsByPod(wsel), "{{pod}}").
		Unit(units.Short).
		Span(12).Height(8)
}

func wlWaitingReasons(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Containers Waiting by Reason", queries.WaitingReasonsByPod(wsel), "{{pod}} · {{reason}}").
		Description("Containers stuck waiting — CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError. Zero for every pod is the healthy state.").
		Unit(units.Short).
		Span(12).Height(8)
}
