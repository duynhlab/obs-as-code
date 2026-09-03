package prometheus_test

import (
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/promql"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
)

// TestVariableQueriesAreWrappedPromQL checks the two halves that make a variable
// query safe, because neither half alone is enough.
//
// The outer label_values() is a Grafana datasource function, not PromQL, so the
// promql gate cannot be pointed at these expressions directly — it rejects them.
// What must hold is that the inner series selector IS valid PromQL, since that is
// the part the datasource evaluates. Variable queries reached production
// unchecked precisely because nothing looked inside the wrapper.
func TestVariableQueriesAreWrappedPromQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		expr  string
		inner string
	}{
		{
			name:  "namespace",
			expr:  queries.NamespaceValues(),
			inner: "kube_pod_info",
		},
		{
			name:  "workload type",
			expr:  queries.WorkloadTypeValues("$namespace"),
			inner: queries.OwnershipRule + `{namespace=~"$namespace"}`,
		},
		{
			name:  "workload",
			expr:  queries.WorkloadValues("$namespace", "$workload_type"),
			inner: queries.OwnershipRule + `{namespace=~"$namespace",workload_type=~"$workload_type"}`,
		},
		{name: "job", expr: queries.JobValues(), inner: "up"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.HasPrefix(tt.expr, "label_values(") || !strings.HasSuffix(tt.expr, ")") {
				t.Fatalf("expr = %q, want a label_values(...) wrapper", tt.expr)
			}
			if !strings.Contains(tt.expr, tt.inner) {
				t.Errorf("expr = %q, want it to select over %q", tt.expr, tt.inner)
			}

			// The inner selector must parse. A variable whose series selector is
			// malformed yields an empty dropdown, and an empty dropdown means
			// every panel filtering on it matches nothing.
			if err := promql.Check(tt.inner); err != nil {
				t.Errorf("inner selector %q does not parse: %v", tt.inner, err)
			}
		})
	}
}

// TestLabelValuesRejectsAnEmptySeries guards the one input that would produce a
// syntactically valid query returning nothing at all.
func TestLabelValuesRejectsAnEmptySeries(t *testing.T) {
	t.Parallel()

	for _, series := range []string{"", "   "} {
		if _, err := queries.LabelValues(series, "namespace"); err == nil {
			t.Errorf("LabelValues(%q, ...) = nil error, want a rejection", series)
		}
	}
	if _, err := queries.LabelValues("up", ""); err == nil {
		t.Error("LabelValues with an empty label = nil error, want a rejection")
	}
}
