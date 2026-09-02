package prometheus_test

import (
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/promql"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
)

// wsel is the selector shape the workloads board passes: every field a Grafana
// variable reference.
var wsel = queries.WorkloadSelector{
	Namespace:    "$namespace",
	WorkloadType: "$workload_type",
	Workload:     "$workload",
}

func TestWorkloadSelectorRejectsEmptyNamespace(t *testing.T) {
	t.Parallel()
	if err := (queries.WorkloadSelector{}).Validate(); err == nil {
		t.Fatal("Validate() accepted an unscoped workload selector")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("CPUByWorkload() did not panic for an unsafe selector")
		}
	}()
	queries.CPUByWorkload(queries.WorkloadSelector{})
}

func workloadQueries() map[string]string {
	return map[string]string{
		"CPUByWorkload":        queries.CPUByWorkload(wsel),
		"MemoryByWorkload":     queries.MemoryByWorkload(wsel),
		"RestartsByWorkload":   queries.RestartsByWorkload(wsel),
		"CPUByPod":             queries.CPUByPod(wsel),
		"MemoryByPod":          queries.MemoryByPod(wsel),
		"RestartsByPod":        queries.RestartsByPod(wsel),
		"ThrottlingByPod":      queries.ThrottlingByPod(wsel),
		"CPUVsRequestsByPod":   queries.CPUVsRequestsByPod(wsel),
		"MemoryVsLimitsByPod":  queries.MemoryVsLimitsByPod(wsel),
		"NetworkReceiveByPod":  queries.NetworkReceiveByPod(wsel),
		"NetworkTransmitByPod": queries.NetworkTransmitByPod(wsel),
		"WaitingReasonsByPod":  queries.WaitingReasonsByPod(wsel),
	}
}

// nodeAndNetworkQueries are the overview additions that ride the same change.
func nodeAndNetworkQueries() map[string]string {
	return map[string]string{
		"NodesUnschedulable":              queries.NodesUnschedulable(),
		"NodePressure":                    queries.NodePressure(),
		"PodsPerNode":                     queries.PodsPerNode(),
		"PodCapacityPerNode":              queries.PodCapacityPerNode(),
		"CPURequestsVsAllocatableByNode":  queries.CPURequestsVsAllocatableByNode(),
		"MemRequestsVsAllocatableByNode":  queries.MemoryRequestsVsAllocatableByNode(),
		"NetworkReceiveDropsByNamespace":  queries.ContainerNetworkReceiveDropsByNamespace(ns),
		"NetworkTransmitDropsByNamespace": queries.ContainerNetworkTransmitDropsByNamespace(ns),
	}
}

func TestEveryWorkloadQueryParses(t *testing.T) {
	t.Parallel()

	all := workloadQueries()
	for name, expr := range nodeAndNetworkQueries() {
		all[name] = expr
	}

	for name, expr := range all {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := promql.Check(expr); err != nil {
				t.Errorf("%s: %v\nexpression: %s", name, err, expr)
			}
		})
	}
}

// TestWorkloadQueriesJoinThroughTheOwnershipRule pins the cross-repo contract:
// every workload query is keyed on the recording rule evaluated in homelab. If
// this name drifts from workload-recording-rules.yaml over there, the failure
// is an empty $workload dropdown and empty panels — this test at least makes
// the dependency visible in one place.
func TestWorkloadQueriesJoinThroughTheOwnershipRule(t *testing.T) {
	t.Parallel()

	if queries.OwnershipRule != "namespace_workload_pod:kube_pod_owner:relabel" {
		t.Errorf("OwnershipRule = %q — this is kubernetes-mixin's canonical name and homelab's rule file uses it; changing one side silently empties the workloads board", queries.OwnershipRule)
	}

	for name, expr := range workloadQueries() {
		if !strings.Contains(expr, queries.OwnershipRule) {
			t.Errorf("%s does not join through the ownership rule: %s", name, expr)
		}
	}
}

// TestWorkloadQueriesRespectTheLabelConvention: the ownership rule and cAdvisor
// carry CLEAN namespace/pod labels — bare `namespace=~"$namespace"` on them is
// correct. Raw kube-state-metrics series are the opposite: their identity is in
// exported_*, so any query touching one must normalize it.
func TestWorkloadQueriesRespectTheLabelConvention(t *testing.T) {
	t.Parallel()

	// These four consume raw KSM series and must carry both the exported_*
	// matcher and the label_replace normalization.
	ksmBacked := map[string]string{
		"RestartsByWorkload":  queries.RestartsByWorkload(wsel),
		"RestartsByPod":       queries.RestartsByPod(wsel),
		"CPUVsRequestsByPod":  queries.CPUVsRequestsByPod(wsel),
		"MemoryVsLimitsByPod": queries.MemoryVsLimitsByPod(wsel),
	}
	for name, expr := range ksmBacked {
		if !strings.Contains(expr, `exported_namespace=~"$namespace"`) {
			t.Errorf("%s reads a raw KSM series without scoping exported_namespace: %s", name, expr)
		}
		if !strings.Contains(expr, `label_replace`) {
			t.Errorf("%s does not normalize exported_* before joining: %s", name, expr)
		}
	}

	// Pure cAdvisor consumers must be scoped by the plain namespace label and
	// the kubelet job.
	for name, expr := range map[string]string{
		"CPUByWorkload": queries.CPUByWorkload(wsel),
		"CPUByPod":      queries.CPUByPod(wsel),
	} {
		if !strings.Contains(expr, `namespace=~"$namespace"`) {
			t.Errorf("%s is not namespace-scoped: %s", name, expr)
		}
		if !strings.Contains(expr, `job="kubelet"`) {
			t.Errorf("%s is not scoped to job=kubelet: %s", name, expr)
		}
	}
}

func TestWorkloadRatioQueriesAreGuarded(t *testing.T) {
	t.Parallel()

	for name, expr := range map[string]string{
		"CPUVsRequestsByPod":  queries.CPUVsRequestsByPod(wsel),
		"MemoryVsLimitsByPod": queries.MemoryVsLimitsByPod(wsel),
	} {
		if !strings.Contains(expr, "> 0)") {
			t.Errorf("%s has no divide-by-zero guard: %s", name, expr)
		}
	}
	if expr := queries.ThrottlingByPod(wsel); !strings.Contains(expr, "clamp_min(") {
		t.Errorf("ThrottlingByPod has no clamp_min guard: %s", expr)
	}
}

func TestNodeQueriesScopeTheJob(t *testing.T) {
	t.Parallel()

	for name, expr := range nodeAndNetworkQueries() {
		if !strings.Contains(expr, `job="`) {
			t.Errorf("%s has no job matcher: %s", name, expr)
		}
	}
}
