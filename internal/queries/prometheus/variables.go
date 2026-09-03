package prometheus

import (
	"fmt"
	"strings"
)

// Grafana variable queries live here for the same reason panel queries do: a
// board that builds its own PromQL is a board whose PromQL nothing checks.
//
// This was not theoretical. The five variable expressions these functions
// replace were written inline in the dashboard packages, which put them outside
// both the house rule ("PromQL lives only in internal/queries") and the
// conformance gate — promql.Check had exactly one call site, and it covered
// panel targets only. Both bugs that shipped an unusable board lived in
// variables: a datasource name where a uid belonged, and an empty selection
// Grafana read as a real choice. Neither could have been caught by a gate that
// never looked at a variable.
//
// The wrapper is a Grafana datasource function rather than PromQL, so the gate
// cannot be aimed at a whole variable query — it rejects label_values() outright.
// What is checkable, and what check.RuleVariableQuery checks, is the series
// selector inside it.

// LabelValues renders a Grafana label_values() variable query over series.
//
// It returns an error rather than a best-effort string because an empty series
// or label produces a query that is syntactically fine and returns nothing —
// which surfaces as an empty dropdown, and then as panels that match nothing.
func LabelValues(series, label string) (string, error) {
	if strings.TrimSpace(series) == "" {
		return "", fmt.Errorf("label_values: series selector is empty")
	}
	if strings.TrimSpace(label) == "" {
		return "", fmt.Errorf("label_values: label is empty")
	}
	return fmt.Sprintf("label_values(%s, %s)", series, label), nil
}

// mustLabelValues is for call sites whose arguments are compile-time constants,
// where an error can only mean this package was edited wrongly.
func mustLabelValues(series, label string) string {
	out, err := LabelValues(series, label)
	if err != nil {
		panic("queries: " + err.Error())
	}
	return out
}

// NamespaceValues lists every namespace that currently has a pod.
//
// It reads exported_namespace, not namespace: kube-state-metrics is scraped
// without honorLabels here, so `namespace` carries the namespace of the KSM pod
// itself — one value, kube-system — while the workload's own namespace lands in
// exported_namespace. See the note above ksmNamespace in kubernetes.go.
func NamespaceValues() string { return mustLabelValues("kube_pod_info", ksmNamespace) }

// JobValues lists every scrape job, for boards parameterised by service.
func JobValues() string { return mustLabelValues("up", "job") }

// WorkloadTypeValues lists the workload kinds present in namespace.
func WorkloadTypeValues(namespace string) string {
	return mustLabelValues(fmt.Sprintf("%s{namespace=~%q}", OwnershipRule, namespace), "workload_type")
}

// WorkloadValues lists the workloads of workloadType in namespace.
func WorkloadValues(namespace, workloadType string) string {
	return mustLabelValues(
		fmt.Sprintf("%s{namespace=~%q,workload_type=~%q}", OwnershipRule, namespace, workloadType),
		"workload",
	)
}
