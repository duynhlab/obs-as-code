// Package kubernetes holds the cluster-level boards.
package kubernetes

import (
	"github.com/grafana/grafana-foundation-sdk/go/cog"
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

// uid is preserved from the hand-written JSON this board replaces, so existing
// URLs, bookmarks and any link pointing at the board keep working.
const uid = "kubernetes-cluster-overview"

// namespaceVar scopes the per-namespace panels. Without it the container panels
// render one series per namespace in the cluster with a table legend computing
// statistics for each — which is what the previous board did.
const namespaceVar = "$namespace"

// meta is declared once and used both to register the board and to build it.
//
// No folder: this repo emits dashboard JSON, and where a board is filed belongs
// to the Grafana it is installed into. The GrafanaDashboard resource in homelab
// sets spec.folder to "Platform / Infrastructure".
var meta = registry.Meta{
	UID:     uid,
	Title:   "Kubernetes Cluster Overview",
	Owner:   "platform",
	Publish: true,
}

// Dashboards returns the Kubernetes domain catalog entries.
func Dashboards() []registry.Dashboard {
	delivery := &registry.Delivery{FolderUID: "platform-infrastructure"}
	return []registry.Dashboard{
		{Meta: meta, Build: func(p profile.Profile) cog.Builder[dashboardv2.Dashboard] { return build(p) }, Delivery: delivery},
		{Meta: workloadsMeta, Build: func(p profile.Profile) cog.Builder[dashboardv2.Dashboard] { return buildWorkloads(p) }, Delivery: delivery},
	}
}

func build(p profile.Profile) *common.DashboardBuilder {
	return common.NewDashboard(p, meta, "kubernetes", "cluster").
		QueryVariable(namespaceVariable(p)).
		Row("Cluster").
		Panel("nodes-ready", nodesReady(p)).
		Panel("pods-running", podsRunning(p)).
		Panel("pods-pending", podsPending(p)).
		Panel("pods-failed", podsFailed(p)).
		Panel("cpu-requests", cpuRequests(p)).
		Panel("memory-requests", memoryRequests(p)).
		Row("Nodes").
		Panel("pods-per-node", podsPerNode(p)).
		Panel("cpu-requests-per-node", cpuRequestsPerNode(p)).
		Panel("memory-requests-per-node", memoryRequestsPerNode(p)).
		Panel("node-pressure", nodePressure(p)).
		Panel("nodes-unschedulable", nodesUnschedulable(p)).
		Row("Workload health").
		Panel("deployments-unavailable", deploymentsUnavailable(p)).
		Panel("statefulsets-unavailable", statefulSetsUnavailable(p)).
		Panel("crashlooping", crashLooping(p)).
		Panel("oom-killed", oomKilled(p)).
		Panel("pod-restarts", podRestarts(p)).
		Panel("pod-phases", podPhases(p)).
		Row("Resource usage").
		Panel("container-cpu", containerCPU(p)).
		Panel("container-memory", containerMemory(p)).
		Panel("cpu-throttling", cpuThrottling(p)).
		Row("Networking").
		Panel("network-io", network(p)).
		Panel("network-drops", networkDrops(p)).
		// The per-workload and per-pod views live on their own board, mixin
		// style, so this one stays cluster-scoped and cardinality-bounded.
		Link(dashboardv2.NewDashboardLinkBuilder().Title("Workloads drill-down").
			Type(dashboardv2.DashboardLinkTypeLink).
			Icon("dashboard").
			Url("/d/kubernetes-workloads"))
}

// namespaceVariable is driven by exported_namespace, the label that actually
// carries a workload's namespace on kube-state-metrics series in this cluster.
// The values are plain namespace names, so the same variable scopes cAdvisor
// panels through their own `namespace` label.
func namespaceVariable(p profile.Profile) *dashboardv2.QueryVariableBuilder {
	return common.SelectAll(dashboardv2.NewQueryVariableBuilder("namespace").
		Label("Namespace").
		Query(prometheus.NewQueryV2Builder().Datasource(p.MetricsRef()).Expr("label_values(kube_pod_info, exported_namespace)")).
		Refresh(dashboardv2.VariableRefreshOnTimeRangeChanged).
		Multi(true).
		Sort(dashboardv2.VariableSortAlphabeticalAsc))
}

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

func nodesReady(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Nodes Ready", queries.NodesReady(), "Ready").
		Description("Nodes reporting Ready. Counts readiness rather than existence, so a node going NotReady moves this number — count(kube_node_info) would not.").
		Unit(units.Short).
		Span(4).Height(4)
}

func podsRunning(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Running Pods", queries.PodsInPhase("Running"), "Running").
		Unit(units.Short).
		Span(4).Height(4)
}

func podsPending(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Pending Pods", queries.PodsInPhase("Pending"), "Pending").
		Description("Pods waiting to be scheduled. A steady non-zero value usually means no node can satisfy the requests — compare against the allocatable gauges.").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "yellow"})).
		Span(4).Height(4)
}

func podsFailed(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Failed Pods", queries.PodsInPhase("Failed"), "Failed").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "red"})).
		Span(4).Height(4)
}

// allocatableCaveat is attached to both request gauges. The number is truthful
// about scheduling and misleading about physical headroom, and the person
// reading the dial is the person who needs to know that.
const allocatableCaveat = " Measures scheduling headroom, not physical headroom: this is a Kind cluster whose nodes share one host, so summed allocatable reports the host's capacity once per node. The scheduler really does decide per node, so this number governs whether the next pod schedules — it does not tell you whether the host will cope."

func cpuRequests(p profile.Profile) *panels.Panel {
	return panels.Gauge(p, "CPU Requests vs Allocatable", queries.CPURequestsVsAllocatable(), "CPU").
		Description("CPU requested by running pods, as a fraction of allocatable CPU." + allocatableCaveat).
		Unit(units.PercentUnit).
		Min(0).Max(1).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.7, Color: "yellow"},
			panels.ThresholdStep{At: 0.85, Color: "red"})).
		Span(4).Height(4)
}

func memoryRequests(p profile.Profile) *panels.Panel {
	return panels.Gauge(p, "Memory Requests vs Allocatable", queries.MemoryRequestsVsAllocatable(), "Memory").
		Description("Memory requested by running pods, as a fraction of allocatable memory." + allocatableCaveat).
		Unit(units.PercentUnit).
		Min(0).Max(1).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.7, Color: "yellow"},
			panels.ThresholdStep{At: 0.85, Color: "red"})).
		Span(4).Height(4)
}

// ---------------------------------------------------------------------------
// Workload health
// ---------------------------------------------------------------------------

func deploymentsUnavailable(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Deployments Degraded", queries.DeploymentsUnavailable(), "Degraded").
		Description("Deployments with at least one replica unavailable. Uses the unavailable-replicas metric rather than comparing desired against ready, because a Deployment with zero ready pods can be missing its ready series entirely — the worst case would otherwise not register.").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "red"})).
		Span(6).Height(4)
}

func statefulSetsUnavailable(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "StatefulSets Degraded", queries.StatefulSetsUnavailable(), "Degraded").
		Description("StatefulSets with fewer ready replicas than desired. The ready count is defaulted to zero when its series is absent, for the same reason as the Deployments panel.").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "red"})).
		Span(6).Height(4)
}

func crashLooping(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "CrashLooping Containers", queries.PodsCrashLooping("5m"), "CrashLooping").
		Description("Containers seen in CrashLoopBackOff in the last 5 minutes. A lookback rather than an instant read, because a crashlooping container spends part of each cycle Running and an instant query catches it only sometimes.").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "red"})).
		Span(6).Height(4)
}

func oomKilled(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Containers OOMKilled (1h)", queries.ContainersOOMKilled("1h"), "OOMKilled").
		Description("Containers whose most recent termination was an OOM kill, in the last hour. This is a count of containers in that state, not a count of OOM events — the underlying metric is a 0/1 gauge and cannot answer the latter.").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "red"})).
		Span(6).Height(4)
}

func podRestarts(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Container Restarts by Namespace", queries.PodRestartsByNamespace(namespaceVar), "{{namespace}}").
		Description("Container restarts per second. A rate rather than an increase over the dashboard range, so the graph shows when restarts happened rather than a flat total.").
		Unit(units.Short).
		Span(12).Height(8)
}

func podPhases(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Pods by Phase and Namespace", queries.PodsByPhaseAndNamespace(namespaceVar), "{{namespace}} · {{phase}}").
		Unit(units.Short).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(12).Height(8)
}

// ---------------------------------------------------------------------------
// Nodes
//
// KSM-only, and every description says so: this cluster has no node-exporter
// (a documented Kind scope-out) and does not scrape /metrics/resource, so
// there is no source of true node CPU or memory utilisation. What Kubernetes
// *believes* about its nodes is all that can be shown honestly.
// ---------------------------------------------------------------------------

func podsPerNode(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Pods per Node", queries.PodsPerNode(), "{{node}}").
		WithTarget(panels.Target(p, queries.PodCapacityPerNode(), "capacity {{node}}")).
		Description("Scheduled pods against each node's allocatable pod slots.").
		Unit(units.Short).
		Span(8).Height(8)
}

func cpuRequestsPerNode(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "CPU Requests vs Allocatable by Node", queries.CPURequestsVsAllocatableByNode(), "{{node}}").
		Description("Fraction of each node's allocatable CPU claimed by running pods' requests. The per-node view is the honest one here: Kind nodes share one host, and the scheduler decides per node." + " No true node utilisation exists on this cluster — no node-exporter, by documented scope-out.").
		Unit(units.PercentUnit).
		Min(0).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.7, Color: "yellow"},
			panels.ThresholdStep{At: 0.85, Color: "red"})).
		Span(8).Height(8)
}

func memoryRequestsPerNode(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Memory Requests vs Allocatable by Node", queries.MemoryRequestsVsAllocatableByNode(), "{{node}}").
		Description("Memory equivalent of the CPU panel beside it, with the same caveat: scheduling headroom, not physical headroom.").
		Unit(units.PercentUnit).
		Min(0).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.7, Color: "yellow"},
			panels.ThresholdStep{At: 0.85, Color: "red"})).
		Span(8).Height(8)
}

func nodePressure(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Node Pressure Conditions", queries.NodePressure(), "{{node}} · {{condition}}").
		Description("Memory, disk and PID pressure conditions currently true, by node. Flat at zero is the healthy state.").
		Unit(units.Short).
		Span(18).Height(6)
}

func nodesUnschedulable(p profile.Profile) *panels.Panel {
	return panels.Stat(p, "Nodes Unschedulable", queries.NodesUnschedulable(), "Cordoned").
		Description("Cordoned nodes. The series is one 0/1 gauge per node and always present, so this reads 0 with no anchor needed.").
		Unit(units.Short).
		Thresholds(panels.Thresholds("green", panels.ThresholdStep{At: 1, Color: "yellow"})).
		Span(6).Height(6)
}

// ---------------------------------------------------------------------------
// Resource usage
// ---------------------------------------------------------------------------

func containerCPU(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "CPU Usage by Namespace", queries.ContainerCPUByNamespace(namespaceVar), "{{namespace}}").
		Description("CPU cores consumed, from cAdvisor. 1 means one core fully used.").
		Unit(units.Short).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(8).Height(8)
}

func containerMemory(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Memory Usage by Namespace", queries.ContainerMemoryByNamespace(namespaceVar), "{{namespace}}").
		Description("Working-set memory, which is the figure the kernel's OOM killer acts on — so it is the figure that predicts a kill.").
		Unit(units.BytesSI).
		Stacking(sdkcommon.NewStackingConfigBuilder().Mode(sdkcommon.StackingModeNormal)).
		Span(8).Height(8)
}

func cpuThrottling(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "CPU Throttling by Namespace", queries.ContainerCPUThrottlingByNamespace(namespaceVar), "{{namespace}}").
		Description("Fraction of CFS periods in which containers were throttled. Grouped by namespace rather than pod to keep the series count bounded, and healthy namespaces are kept visible at zero rather than filtered out.").
		Unit(units.PercentUnit).
		Min(0).
		Thresholds(panels.Thresholds("green",
			panels.ThresholdStep{At: 0.25, Color: "yellow"},
			panels.ThresholdStep{At: 0.5, Color: "red"})).
		Span(8).Height(8)
}

func network(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Network I/O by Namespace", queries.ContainerNetworkReceiveByNamespace(namespaceVar), "RX {{namespace}}").
		WithTarget(panels.Target(p, queries.ContainerNetworkTransmitByNamespace(namespaceVar), "TX {{namespace}}")).
		Description("Pod network throughput. Transmit is drawn below the axis so the two directions can be read against each other rather than summed by eye.").
		Unit(units.BytesPerSecondSI).
		// Min is unset here: with TX mirrored below zero, clamping the axis at 0
		// would hide exactly the half this override exists to show.
		WithOverride(
			dashboardv2.MatcherConfig{Id: "byRegexp", Options: "TX.*"},
			[]dashboardv2.DynamicConfigValue{
				// No typed builder exists for a custom field-config property, so
				// this is the documented escape hatch rather than a workaround.
				{Id: "custom.transform", Value: "negative-Y"},
			}).
		Span(12).Height(8)
}

func networkDrops(p profile.Profile) *panels.Panel {
	return panels.Timeseries(p, "Packets Dropped by Namespace", queries.ContainerNetworkReceiveDropsByNamespace(namespaceVar), "RX {{namespace}}").
		WithTarget(panels.Target(p, queries.ContainerNetworkTransmitDropsByNamespace(namespaceVar), "TX {{namespace}}")).
		Description("Packets dropped per second — queueing or conntrack pressure that the bandwidth panel hides. Transmit is drawn below the axis, matching the I/O panel.").
		Unit(units.Short).
		WithOverride(
			dashboardv2.MatcherConfig{Id: "byRegexp", Options: "TX.*"},
			[]dashboardv2.DynamicConfigValue{{Id: "custom.transform", Value: "negative-Y"}}).
		Span(12).Height(8)
}
