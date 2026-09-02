package prometheus

import "fmt"

// OwnershipRule is kubernetes-mixin's pod→workload mapping, evaluated by
// vmalert in homelab:
//
//	homelab/kubernetes/infra/configs/observability/metrics/prometheusrules/kubernetes/workload-recording-rules.yaml
//
// The rule absorbs the exported_* rename inside itself, so its series carry
// CLEAN namespace/workload/pod labels — every join below is a plain
// `on (namespace, pod)`, and the $workload/$workload_type dashboard variables
// are label_values() over it.
//
// This name is the cross-repo contract. If the homelab rule is renamed or
// missing, the failure mode is an empty $workload dropdown and empty workload
// panels — start at the file above, then
// `count(namespace_workload_pod:kube_pod_owner:relabel)` against VMSingle.
const OwnershipRule = "namespace_workload_pod:kube_pod_owner:relabel"

// WorkloadSelector scopes workload-level queries. Fields hold Grafana variable
// references and land inside matcher values, the only position the query checks
// allow.
type WorkloadSelector struct {
	// Namespace is required: an unscoped workload query walks the cluster.
	Namespace string

	// WorkloadType optionally narrows to deployment/statefulset/daemonset/….
	WorkloadType string

	// Workload optionally narrows to one workload.
	Workload string
}

// ownership renders the recording-rule selector for s.
func (s WorkloadSelector) ownership() string {
	out := fmt.Sprintf(`%s{namespace=~%q`, OwnershipRule, s.Namespace)
	if s.WorkloadType != "" {
		out += fmt.Sprintf(`,workload_type=~%q`, s.WorkloadType)
	}
	if s.Workload != "" {
		out += fmt.Sprintf(`,workload=~%q`, s.Workload)
	}
	return out + "}"
}

// joinWorkload attaches workload identity to a cAdvisor-shaped expression
// (clean namespace/pod labels) and aggregates by workload.
func (s WorkloadSelector) joinWorkload(expr string) string {
	return fmt.Sprintf(
		`sum by (workload, workload_type) (%s * on (namespace, pod) group_left (workload, workload_type) (%s))`,
		expr, s.ownership())
}

// joinPods keeps per-pod granularity while restricting to the selected
// workload's pods.
func (s WorkloadSelector) joinPods(expr string) string {
	return fmt.Sprintf(
		`sum by (pod) (%s * on (namespace, pod) group_left () (%s))`,
		expr, s.ownership())
}

// normalizePodLabels rewrites a kube-state-metrics expression's exported_*
// identity onto the plain labels, so it can join the ownership rule and
// cAdvisor. This is the inverse direction of the rename the scrape performs;
// see the ksmNamespace comment in kubernetes.go for why the rename exists.
func normalizePodLabels(expr string) string {
	return fmt.Sprintf(
		`label_replace(label_replace(%s, "namespace", "$1", %q, "(.+)"), "pod", "$1", %q, "(.+)")`,
		expr, ksmNamespace, ksmPod)
}

// ---------------------------------------------------------------------------
// By workload
// ---------------------------------------------------------------------------

// CPUByWorkload is CPU cores consumed, by workload.
func CPUByWorkload(s WorkloadSelector) string {
	return s.joinWorkload(cadvisorCPU(s.Namespace))
}

// MemoryByWorkload is working-set memory, by workload.
func MemoryByWorkload(s WorkloadSelector) string {
	return s.joinWorkload(cadvisorMemory(s.Namespace))
}

// RestartsByWorkload is container restarts per second, by workload.
func RestartsByWorkload(s WorkloadSelector) string {
	return s.joinWorkload(ksmRestarts(s.Namespace))
}

// ---------------------------------------------------------------------------
// By pod
// ---------------------------------------------------------------------------

// CPUByPod is CPU cores consumed by each pod of the selected workload.
func CPUByPod(s WorkloadSelector) string {
	return s.joinPods(cadvisorCPU(s.Namespace))
}

// MemoryByPod is working-set memory by pod.
func MemoryByPod(s WorkloadSelector) string {
	return s.joinPods(cadvisorMemory(s.Namespace))
}

// RestartsByPod is container restarts per second, by pod.
func RestartsByPod(s WorkloadSelector) string {
	return s.joinPods(ksmRestarts(s.Namespace))
}

// ThrottlingByPod is the fraction of CFS periods throttled, by pod, for the
// selected workload's pods. Same clamp_min guard as the namespace-level panel:
// a pod without a CPU limit reports no periods, and 0/0 must read as 0, not
// churn the series set.
func ThrottlingByPod(s WorkloadSelector) string {
	matchers := cadvisorMatchers(s.Namespace, `container!=""`)

	throttled := s.joinPods(fmt.Sprintf(
		`rate(container_cpu_cfs_throttled_periods_total%s[%s])`, matchers, rateInterval))
	total := s.joinPods(fmt.Sprintf(
		`rate(container_cpu_cfs_periods_total%s[%s])`, matchers, rateInterval))

	return fmt.Sprintf(`%s / clamp_min(%s, 1)`, throttled, total)
}

// CPUVsRequestsByPod is each pod's CPU usage as a fraction of its requests —
// above 1 is legitimate burst; sustained far below 1 is over-provisioning.
func CPUVsRequestsByPod(s WorkloadSelector) string {
	usage := s.joinPods(cadvisorCPU(s.Namespace))
	requests := s.joinPods(normalizePodLabels(fmt.Sprintf(
		`kube_pod_container_resource_requests%s`,
		ksmMatchers(s.Namespace, `resource="cpu"`))))

	return fmt.Sprintf(`%s / (%s > 0)`, usage, requests)
}

// MemoryVsLimitsByPod is each pod's working set as a fraction of its memory
// limit — the number the OOM killer acts on. Reuses the join shape proven by
// homelab's KubePodMemoryNearLimit alert (the one fixed after being "dead
// fleet-wide" in the 2026-07-17 audit).
func MemoryVsLimitsByPod(s WorkloadSelector) string {
	usage := s.joinPods(cadvisorMemory(s.Namespace))
	limits := s.joinPods(normalizePodLabels(fmt.Sprintf(
		`kube_pod_container_resource_limits%s`,
		ksmMatchers(s.Namespace, `resource="memory"`))))

	return fmt.Sprintf(`%s / (%s > 0)`, usage, limits)
}

// NetworkReceiveByPod is inbound bytes per second by pod. Network counters live
// on the pod sandbox, so the namespace!="" guard is the correct one here, as on
// the namespace-level panel.
func NetworkReceiveByPod(s WorkloadSelector) string {
	return s.joinPods(fmt.Sprintf(
		`rate(container_network_receive_bytes_total%s[%s])`,
		cadvisorMatchers(s.Namespace, `namespace!=""`), rateInterval))
}

// NetworkTransmitByPod is the outbound equivalent.
func NetworkTransmitByPod(s WorkloadSelector) string {
	return s.joinPods(fmt.Sprintf(
		`rate(container_network_transmit_bytes_total%s[%s])`,
		cadvisorMatchers(s.Namespace, `namespace!=""`), rateInterval))
}

// WaitingReasonsByPod shows containers stuck waiting, by pod and reason —
// CrashLoopBackOff, ImagePullBackOff, CreateContainerConfigError and friends.
func WaitingReasonsByPod(s WorkloadSelector) string {
	waiting := normalizePodLabels(fmt.Sprintf(
		`kube_pod_container_status_waiting_reason%s`, ksmMatchers(s.Namespace)))

	return fmt.Sprintf(
		`sum by (pod, reason) (%s * on (namespace, pod) group_left () (%s))`,
		waiting, s.ownership())
}

// ---------------------------------------------------------------------------
// Shared fragments
// ---------------------------------------------------------------------------

// cadvisorCPU is the per-(namespace,pod) CPU rate every join above starts from.
func cadvisorCPU(namespace string) string {
	return fmt.Sprintf(`sum by (namespace, pod) (rate(container_cpu_usage_seconds_total%s[%s]))`,
		cadvisorMatchers(namespace, `container!=""`, `image!=""`), rateInterval)
}

// cadvisorMemory is the per-(namespace,pod) working set.
func cadvisorMemory(namespace string) string {
	return fmt.Sprintf(`sum by (namespace, pod) (container_memory_working_set_bytes%s)`,
		cadvisorMatchers(namespace, `container!=""`, `image!=""`))
}

// ksmRestarts is the per-(namespace,pod) restart rate, normalized from the
// exported_* labels so it joins like a cAdvisor series.
func ksmRestarts(namespace string) string {
	return fmt.Sprintf(`sum by (namespace, pod) (%s)`,
		normalizePodLabels(fmt.Sprintf(
			`rate(kube_pod_container_status_restarts_total%s[%s])`,
			ksmMatchers(namespace), rateInterval)))
}
