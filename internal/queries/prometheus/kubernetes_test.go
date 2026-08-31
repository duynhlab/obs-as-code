package prometheus_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/promql"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
)

// ns is the namespace variable reference a board passes in.
const ns = "$namespace"

// bareNamespace matches a namespace matcher or by() clause that is not the
// renamed exported_namespace. The leading class rules out an identifier
// character before the word, which is what distinguishes the two.
var bareNamespace = regexp.MustCompile(`(^|[^_0-9A-Za-z"])namespace\s*(=~|=|!=|!~)|by\s*\(\s*namespace\b`)

// kubernetesQueries is every expression the Kubernetes board uses. Listing them
// here is what makes the parse and convention checks below exhaustive.
func kubernetesQueries() map[string]string {
	return map[string]string{
		"NodesReady":                        queries.NodesReady(),
		"NodesNotReady":                     queries.NodesNotReady(),
		"PodsRunning":                       queries.PodsInPhase("Running"),
		"PodsPending":                       queries.PodsInPhase("Pending"),
		"PodsFailed":                        queries.PodsInPhase("Failed"),
		"CPURequestsVsAllocatable":          queries.CPURequestsVsAllocatable(),
		"MemoryRequestsVsAllocatable":       queries.MemoryRequestsVsAllocatable(),
		"DeploymentsUnavailable":            queries.DeploymentsUnavailable(),
		"StatefulSetsUnavailable":           queries.StatefulSetsUnavailable(),
		"PodsCrashLooping":                  queries.PodsCrashLooping("5m"),
		"ContainersOOMKilled":               queries.ContainersOOMKilled("1h"),
		"PodRestartsByNamespace":            queries.PodRestartsByNamespace(ns),
		"PodsByPhaseAndNamespace":           queries.PodsByPhaseAndNamespace(ns),
		"ContainerCPUByNamespace":           queries.ContainerCPUByNamespace(ns),
		"ContainerMemoryByNamespace":        queries.ContainerMemoryByNamespace(ns),
		"ContainerCPUThrottlingByNamespace": queries.ContainerCPUThrottlingByNamespace(ns),
		"ContainerNetworkReceive":           queries.ContainerNetworkReceiveByNamespace(ns),
		"ContainerNetworkTransmit":          queries.ContainerNetworkTransmitByNamespace(ns),
	}
}

func TestEveryKubernetesQueryParses(t *testing.T) {
	t.Parallel()

	for name, expr := range kubernetesQueries() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Both parsers: valid for the VictoriaMetrics engine that runs it,
			// and portable to a plain Prometheus datasource.
			if err := promql.Check(expr); err != nil {
				t.Errorf("%s: %v\nexpression: %s", name, err, expr)
			}
		})
	}
}

func TestEveryKubernetesQueryScopesTheJob(t *testing.T) {
	t.Parallel()

	// Every scrape object in this cluster hard-replaces `job` with a literal.
	// An unscoped query multiply-counts the moment a second job scrapes the same
	// exporter, and reports a plausible integer while doing it.
	for name, expr := range kubernetesQueries() {
		if !strings.Contains(expr, `job="`) {
			t.Errorf("%s has no job matcher: %s", name, expr)
		}
	}
}

// TestKubeStateMetricsQueriesUseTheRenamedLabels is the most important test in
// this file. kube-state-metrics is scraped without honorLabels, so a query that
// reads `namespace` off a kube_pod_* series is reading the namespace of the
// kube-state-metrics pod — one series, labelled kube-system, looking entirely
// plausible.
func TestKubeStateMetricsQueriesUseTheRenamedLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
	}{
		{"PodRestartsByNamespace", queries.PodRestartsByNamespace(ns)},
		{"PodsByPhaseAndNamespace", queries.PodsByPhaseAndNamespace(ns)},
		{"CPURequestsVsAllocatable", queries.CPURequestsVsAllocatable()},
		{"MemoryRequestsVsAllocatable", queries.MemoryRequestsVsAllocatable()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(tt.expr, "exported_namespace") {
				t.Errorf("reads a plain namespace label off kube-state-metrics: %s", tt.expr)
			}

			// A *bare* namespace matcher or by() clause on a KSM series is the
			// bug this test exists to catch. Substring matching cannot express
			// that: "exported_namespace=~" contains "namespace=~". The leading
			// boundary excludes an identifier character, so exported_namespace
			// does not match, while the quoted "namespace" that label_replace
			// writes as a display label is left alone.
			if m := bareNamespace.FindString(tt.expr); m != "" {
				t.Errorf("uses a bare namespace label (%q) on a kube-state-metrics series: %s", m, tt.expr)
			}
		})
	}
}

func TestCadvisorQueriesUsePlainLabels(t *testing.T) {
	t.Parallel()

	// The mirror image: cAdvisor comes through the kubelet, whose labels do not
	// collide, so using exported_namespace there would match nothing.
	for name, expr := range map[string]string{
		"ContainerCPUByNamespace":           queries.ContainerCPUByNamespace(ns),
		"ContainerMemoryByNamespace":        queries.ContainerMemoryByNamespace(ns),
		"ContainerCPUThrottlingByNamespace": queries.ContainerCPUThrottlingByNamespace(ns),
		"ContainerNetworkReceive":           queries.ContainerNetworkReceiveByNamespace(ns),
	} {
		if strings.Contains(expr, "exported_") {
			t.Errorf("%s uses an exported_* label on a cAdvisor series: %s", name, expr)
		}
		if !strings.Contains(expr, `job="kubelet"`) {
			t.Errorf("%s is not scoped to job=kubelet: %s", name, expr)
		}
	}
}

func TestRatioQueriesCannotDivideByZero(t *testing.T) {
	t.Parallel()

	// An absent denominator yields +Inf, which renders as a full gauge — the
	// most alarming possible display of "no data".
	for name, expr := range map[string]string{
		"CPURequestsVsAllocatable":    queries.CPURequestsVsAllocatable(),
		"MemoryRequestsVsAllocatable": queries.MemoryRequestsVsAllocatable(),
	} {
		if !strings.Contains(expr, "> 0)") {
			t.Errorf("%s has no divide-by-zero guard: %s", name, expr)
		}
	}

	throttling := queries.ContainerCPUThrottlingByNamespace(ns)
	if !strings.Contains(throttling, "clamp_min(") {
		t.Errorf("throttling ratio has no clamp_min guard: %s", throttling)
	}
	// The original filtered NaN with a trailing `> 0`, which also dropped every
	// healthy namespace and made the series set churn.
	if strings.HasSuffix(strings.TrimSpace(throttling), "> 0") {
		t.Errorf("throttling ratio still filters healthy namespaces away: %s", throttling)
	}
}

func TestRequestsExcludeNonRunningPods(t *testing.T) {
	t.Parallel()

	// kube_pod_container_resource_requests is emitted for Pending, Succeeded and
	// Failed pods, so a finished Job inflates the numerator forever.
	for name, expr := range map[string]string{
		"CPURequestsVsAllocatable":    queries.CPURequestsVsAllocatable(),
		"MemoryRequestsVsAllocatable": queries.MemoryRequestsVsAllocatable(),
	} {
		if !strings.Contains(expr, `phase="Running"`) {
			t.Errorf("%s counts requests of pods that are not running: %s", name, expr)
		}
	}
}

func TestUnavailableWorkloadQueriesSurviveAMissingReadySeries(t *testing.T) {
	t.Parallel()

	// The case that matters most — zero ready replicas — is the case where
	// kube-state-metrics may emit no ready series at all, so a naive
	// `spec != ready` comparison matches nothing and the panel reads healthy.
	deploy := queries.DeploymentsUnavailable()
	if !strings.Contains(deploy, "replicas_unavailable") {
		t.Errorf("DeploymentsUnavailable compares against a series that can be absent: %s", deploy)
	}

	sts := queries.StatefulSetsUnavailable()
	if !strings.Contains(sts, " or ") {
		t.Errorf("StatefulSetsUnavailable does not default a missing ready series to zero: %s", sts)
	}
}

func TestKubernetesRateQueriesUseTheRateIntervalMacro(t *testing.T) {
	t.Parallel()

	for name, expr := range kubernetesQueries() {
		if !strings.Contains(expr, "rate(") {
			continue
		}
		if !strings.Contains(expr, "[$__rate_interval]") {
			t.Errorf("%s uses a rate function with a fixed window: %s", name, expr)
		}
	}
}

// TestBareNamespaceMatcherFires proves the guard above can actually fail. A
// check that cannot fire is worse than no check: it reports success forever and
// nobody looks at it again.
func TestBareNamespaceMatcherFires(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{
			name: "bare matcher is caught",
			expr: `sum(kube_pod_status_phase{job="kube-state-metrics",namespace=~"$namespace"})`,
			want: true,
		},
		{
			name: "bare by clause is caught",
			expr: `sum by (namespace) (kube_pod_status_phase{job="kube-state-metrics"})`,
			want: true,
		},
		{
			name: "bare by clause with a second label is caught",
			expr: `sum by (namespace, phase) (kube_pod_status_phase{job="kube-state-metrics"})`,
			want: true,
		},
		{
			name: "exported_namespace matcher is allowed",
			expr: `sum(kube_pod_status_phase{exported_namespace=~"$namespace"})`,
			want: false,
		},
		{
			name: "exported_namespace by clause is allowed",
			expr: `sum by (exported_namespace) (kube_pod_status_phase)`,
			want: false,
		},
		{
			name: "the quoted display label written by label_replace is allowed",
			expr: `label_replace(x, "namespace", "$1", "exported_namespace", "(.*)")`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := bareNamespace.MatchString(tt.expr); got != tt.want {
				t.Errorf("MatchString(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}
