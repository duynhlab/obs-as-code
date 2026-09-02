package prometheus

import "fmt"

// Scrape job labels, taken from homelab's scrape configuration. These are
// cluster facts, not preferences: every scrape object hard-replaces `job` with a
// literal, so an unscoped query risks multiply-counting if a second job ever
// scrapes the same exporter.
const (
	jobKubeStateMetrics = "kube-state-metrics"

	// jobKubelet covers both kubelet scrapes — /metrics/cadvisor and /metrics —
	// because both VMNodeScrape objects relabel job to the same value.
	jobKubelet = "kubelet"
)

// kube-state-metrics is scraped WITHOUT honorLabels, so a workload's own
// namespace, pod and container labels collide with the scrape target's and are
// renamed. The plain `namespace` and `pod` labels on a kube_pod_* series name
// **the kube-state-metrics pod itself**, which is why a query written the
// obvious way silently collapses every namespace into one series labelled
// kube-system.
//
// Anything reading a workload's identity off a kube_pod_* series must use these
// constants. kube_node_* is unaffected: `node` is not a scrape target label.
//
// The same convention is already load-bearing in homelab's PrometheusRules —
// see prometheusrules/kubernetes/pod-resources-alerts.yaml, which label_replaces
// three times to make a join work.
const (
	ksmNamespace = "exported_namespace"
	ksmPod       = "exported_pod"
)

// cAdvisor is scraped through the kubelet, whose own labels do not collide, so
// container_* series carry a plain, correct `namespace`.
const cadvisorNamespace = "namespace"

// anchoredCount wraps a count() over a sparse bad-thing series so an empty
// result renders as 0 instead of "No data" — found by the live audit of homelab
// PR 963: an operator could not tell "nothing is crashlooping" from "this panel
// is broken".
//
// Anchored on kube_node_info rather than a bare `or on() vector(0)`, because the
// bare form trades one ambiguity for a worse one:
//
//	cluster state           bare vector(0)         anchored
//	healthy, KSM up         0                      0
//	bad things exist        N                      N
//	KSM down / job renamed  0 — reads healthy      blank — outage visible
//
// kube_node_info is the anchor because it is authored by kube-state-metrics
// itself (so it proves KSM is emitting, which `up` alone would not) and exists
// whenever KSM does.
func anchoredCount(countExpr string) string {
	return fmt.Sprintf(`%s or (0 * sum(kube_node_info{job=%q}))`, countExpr, jobKubeStateMetrics)
}

// ---------------------------------------------------------------------------
// Cluster state (kube-state-metrics)
// ---------------------------------------------------------------------------

// NodesReady is the count of nodes reporting Ready.
//
// Deliberately not count(kube_node_info): that counts nodes that exist, which on
// a fixed cluster is a constant that does not budge when a node goes NotReady —
// a stat panel that can never change is a stat panel nobody reads.
func NodesReady() string {
	return fmt.Sprintf(`sum(kube_node_status_condition{job=%q,condition="Ready",status="true"})`, jobKubeStateMetrics)
}

// NodesNotReady is the count of nodes that exist but are not Ready.
func NodesNotReady() string {
	return fmt.Sprintf(`sum(kube_node_status_condition{job=%q,condition="Ready",status!="true"})`, jobKubeStateMetrics)
}

// PodsInPhase counts pods in one lifecycle phase, e.g. "Running", "Pending",
// "Failed".
//
// kube_pod_status_phase emits one series per pod and phase, valued 1 for the
// current phase and 0 for the others, so summing the phase's series is the
// count. No `== 1` filter is needed, and leaving it out means the query still
// reports 0 rather than no-data when a phase is empty.
func PodsInPhase(phase string) string {
	return fmt.Sprintf(`sum(kube_pod_status_phase{job=%q,phase=%q})`, jobKubeStateMetrics, phase)
}

// CPURequestsVsAllocatable is the fraction of allocatable CPU already claimed by
// the requests of running pods.
//
// Two corrections over the obvious form. Requests are joined against running
// pods, because kube_pod_container_resource_requests is emitted for Pending,
// Succeeded and Failed pods too — a finished Job would otherwise keep inflating
// the numerator forever. And the denominator carries a `> 0` guard, without
// which an absent allocatable series yields +Inf rather than no data.
func CPURequestsVsAllocatable() string {
	return requestsVsAllocatable("cpu")
}

// MemoryRequestsVsAllocatable is the memory equivalent of
// CPURequestsVsAllocatable.
func MemoryRequestsVsAllocatable() string {
	return requestsVsAllocatable("memory")
}

func requestsVsAllocatable(resource string) string {
	running := fmt.Sprintf(
		`max by (%[1]s, %[2]s) (kube_pod_status_phase{job=%[3]q,phase="Running"}) == 1`,
		ksmNamespace, ksmPod, jobKubeStateMetrics)

	requests := fmt.Sprintf(
		`sum(kube_pod_container_resource_requests{job=%[1]q,resource=%[2]q} * on (%[3]s, %[4]s) group_left () (%[5]s))`,
		jobKubeStateMetrics, resource, ksmNamespace, ksmPod, running)

	allocatable := fmt.Sprintf(
		`sum(kube_node_status_allocatable{job=%q,resource=%q})`,
		jobKubeStateMetrics, resource)

	return fmt.Sprintf(`%s / (%s > 0)`, requests, allocatable)
}

// ---------------------------------------------------------------------------
// Workload health (kube-state-metrics)
// ---------------------------------------------------------------------------

// DeploymentsUnavailable counts Deployments with at least one replica
// unavailable.
//
// Not count(spec_replicas != ready_replicas): when a Deployment has zero ready
// pods, kube-state-metrics may emit no ready_replicas series at all, so the
// comparison matches nothing and the panel misses the very case it exists to
// catch. The unavailable-replicas metric is always present.
func DeploymentsUnavailable() string {
	return anchoredCount(fmt.Sprintf(`count(kube_deployment_status_replicas_unavailable{job=%q} > 0)`, jobKubeStateMetrics))
}

// StatefulSetsUnavailable counts StatefulSets with fewer ready replicas than
// desired.
//
// There is no unavailable-replicas metric for StatefulSets, so the ready count
// is defaulted to zero with `or ... * 0` for the same reason as above: a
// StatefulSet with no ready pods can be missing its ready series entirely, and
// subtracting an absent series yields nothing rather than "all of them are
// down".
func StatefulSetsUnavailable() string {
	desired := fmt.Sprintf(`kube_statefulset_status_replicas{job=%q}`, jobKubeStateMetrics)
	ready := fmt.Sprintf(`kube_statefulset_status_replicas_ready{job=%q}`, jobKubeStateMetrics)

	return anchoredCount(fmt.Sprintf(`count((%[1]s - (%[2]s or %[1]s * 0)) > 0)`, desired, ready))
}

// PodsCrashLooping counts containers seen in CrashLoopBackOff within the lookback
// window.
//
// max_over_time rather than an instant read: a crashlooping container spends part
// of its cycle in Running, so an instant query catches it only sometimes and the
// stat flickers between 0 and 1.
func PodsCrashLooping(lookback string) string {
	return anchoredCount(fmt.Sprintf(
		`count(max_over_time(kube_pod_container_status_waiting_reason{job=%q,reason="CrashLoopBackOff"}[%s]) == 1)`,
		jobKubeStateMetrics, lookback))
}

// ContainersOOMKilled counts containers whose most recent termination was an OOM
// kill, within the lookback window.
//
// This is a count of containers in that state, not a count of OOM events — the
// underlying metric is a 0/1 gauge and cannot answer the latter. The board this
// replaces applied increase() to that gauge and titled the panel "OOM Events",
// which returns near-zero for a container that has been OOMKilled the whole
// window. A correct number under an honest title beats a wrong number under the
// title someone wanted.
func ContainersOOMKilled(lookback string) string {
	return anchoredCount(fmt.Sprintf(
		`count(max_over_time(kube_pod_container_status_last_terminated_reason{job=%q,reason="OOMKilled"}[%s]) == 1)`,
		jobKubeStateMetrics, lookback))
}

// PodRestartsByNamespace is container restarts per second, by namespace.
//
// $__rate_interval rather than the original's fixed [1h]: an increase over the
// whole dashboard range makes every point identical, so the graph is a flat line
// that cannot show *when* the restarts happened, which is the only thing anyone
// looks at this panel for.
func PodRestartsByNamespace(namespace string) string {
	return namespaceLabel(fmt.Sprintf(
		`sum by (%s) (rate(kube_pod_container_status_restarts_total%s[%s]))`,
		ksmNamespace, ksmMatchers(namespace), rateInterval))
}

// PodsByPhaseAndNamespace is the pod count per namespace and phase.
func PodsByPhaseAndNamespace(namespace string) string {
	return namespaceLabel(fmt.Sprintf(
		`sum by (%s, phase) (kube_pod_status_phase%s)`,
		ksmNamespace, ksmMatchers(namespace)))
}

// ---------------------------------------------------------------------------
// Container resources (cAdvisor, through the kubelet)
// ---------------------------------------------------------------------------

// ContainerCPUByNamespace is CPU cores consumed per namespace.
func ContainerCPUByNamespace(namespace string) string {
	return fmt.Sprintf(
		`sum by (%s) (rate(container_cpu_usage_seconds_total%s[%s]))`,
		cadvisorNamespace, cadvisorMatchers(namespace, `container!=""`, `image!=""`), rateInterval)
}

// ContainerMemoryByNamespace is working-set memory per namespace.
//
// Working set rather than RSS or usage: it is the number the kernel's OOM killer
// acts on, so it is the number that predicts a kill.
func ContainerMemoryByNamespace(namespace string) string {
	return fmt.Sprintf(
		`sum by (%s) (container_memory_working_set_bytes%s)`,
		cadvisorNamespace, cadvisorMatchers(namespace, `container!=""`, `image!=""`))
}

// ContainerCPUThrottlingByNamespace is the fraction of CFS periods in which a
// namespace's containers were throttled.
//
// Grouped by namespace, not by pod: `by (namespace, pod)` is unbounded, and the
// original rendered every pod as a line with a table legend computing mean and
// max per series.
//
// clamp_min on the denominator rather than a trailing `> 0`: containers without
// a CPU limit report no CFS periods, so the ratio is 0/0. The original's `> 0`
// filtered the resulting NaN by accident, and in doing so also dropped every
// healthy namespace — so the series set churned and the legend's statistics were
// computed over a moving population.
func ContainerCPUThrottlingByNamespace(namespace string) string {
	matchers := cadvisorMatchers(namespace, `container!=""`)

	throttled := fmt.Sprintf(`sum by (%s) (rate(container_cpu_cfs_throttled_periods_total%s[%s]))`,
		cadvisorNamespace, matchers, rateInterval)
	total := fmt.Sprintf(`sum by (%s) (rate(container_cpu_cfs_periods_total%s[%s]))`,
		cadvisorNamespace, matchers, rateInterval)

	return fmt.Sprintf(`%s / clamp_min(%s, 1)`, throttled, total)
}

// ContainerNetworkReceiveByNamespace is inbound bytes per second, by namespace.
//
// The guard is namespace!="" rather than the original's image!="": cAdvisor
// reports pod-level network counters on the pod sandbox container, where `image`
// is populated on some container runtimes and empty on others. Filtering on it
// makes the panel silently empty on half the clusters it might run on.
func ContainerNetworkReceiveByNamespace(namespace string) string {
	return containerNetworkByNamespace("receive", namespace)
}

// ContainerNetworkTransmitByNamespace is outbound bytes per second, by namespace.
func ContainerNetworkTransmitByNamespace(namespace string) string {
	return containerNetworkByNamespace("transmit", namespace)
}

func containerNetworkByNamespace(direction, namespace string) string {
	return fmt.Sprintf(
		`sum by (%s) (rate(container_network_%s_bytes_total%s[%s]))`,
		cadvisorNamespace, direction,
		cadvisorMatchers(namespace, `namespace!=""`), rateInterval)
}

// ---------------------------------------------------------------------------
// Matcher helpers
// ---------------------------------------------------------------------------

// ksmMatchers builds a matcher set for a kube-state-metrics pod-level series,
// scoping namespace through the renamed label.
func ksmMatchers(namespace string, extra ...string) string {
	return matchers(jobKubeStateMetrics, ksmNamespace, namespace, extra...)
}

// cadvisorMatchers builds a matcher set for a cAdvisor series.
func cadvisorMatchers(namespace string, extra ...string) string {
	return matchers(jobKubelet, cadvisorNamespace, namespace, extra...)
}

func matchers(job, namespaceLabel, namespace string, extra ...string) string {
	parts := make([]string, 0, 2+len(extra))
	parts = append(parts, fmt.Sprintf("job=%q", job))
	if namespace != "" {
		parts = append(parts, fmt.Sprintf(`%s=~%q`, namespaceLabel, namespace))
	}
	parts = append(parts, extra...)

	out := "{"
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out + "}"
}

// namespaceLabel copies exported_namespace onto a plain `namespace` label so a
// legend can read {{namespace}}. Without it every legend entry on a
// kube-state-metrics panel would read "exported_namespace", which is an
// implementation detail of how this cluster scrapes, not something a viewer
// should have to know.
func namespaceLabel(expr string) string {
	return fmt.Sprintf(`label_replace(%s, "namespace", "$1", %q, "(.*)")`, expr, ksmNamespace)
}
