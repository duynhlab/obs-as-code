package prometheus_test

import (
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/promql"
	queries "github.com/duynhlab/obs-as-code/internal/queries/prometheus"
)

// sel is the selector shape a board actually passes.
var sel = queries.Selector{Job: "$job", Namespace: "$namespace"}

func TestSelectorMatchers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		selector   queries.Selector
		additional []string
		want       string
	}{
		{
			name:     "job only",
			selector: queries.Selector{Job: "$job"},
			want:     `{job=~"$job"}`,
		},
		{
			name:     "job and namespace",
			selector: queries.Selector{Job: "$job", Namespace: "$ns"},
			want:     `{job=~"$job",namespace=~"$ns"}`,
		},
		{
			// Sorted, so the rendered expression does not shuffle between runs
			// and turn every generate into a diff.
			name:     "extra matchers are sorted",
			selector: queries.Selector{Job: "$job", Extra: map[string]string{"zone": "a", "cluster": "c", "pod": "p"}},
			want:     `{job=~"$job",cluster=~"c",pod=~"p",zone=~"a"}`,
		},
		{
			name:       "additional matchers appended last",
			selector:   queries.Selector{Job: "$job"},
			additional: []string{`http_response_status_code=~"5.."`},
			want:       `{job=~"$job",http_response_status_code=~"5.."}`,
		},
		{
			name:     "empty selector renders nothing",
			selector: queries.Selector{},
			want:     ``,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.selector.Matchers(tt.additional...); got != tt.want {
				t.Errorf("Matchers() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectorMatchersIsStable(t *testing.T) {
	t.Parallel()

	// Map iteration order is randomized per run, so this would fail without
	// the explicit sort — and the failure would show up as a phantom diff in
	// generated output rather than as a test failure.
	s := queries.Selector{Job: "$job", Extra: map[string]string{
		"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6",
	}}

	first := s.Matchers()
	for range 50 {
		if got := s.Matchers(); got != first {
			t.Fatalf("Matchers() is unstable: %q then %q", first, got)
		}
	}
}

func TestSelectorValidate(t *testing.T) {
	t.Parallel()

	if err := (queries.Selector{Job: "$job"}).Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	err := (queries.Selector{Namespace: "$ns"}).Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error for a selector with no job")
	}
	if !strings.Contains(err.Error(), "whole cluster") {
		t.Errorf("Validate() = %v, want it to explain the consequence", err)
	}
}

// TestEveryQueryParses is the gate that makes the "no inline PromQL" rule worth
// having: because every expression is produced by a function in this package,
// this one test covers all of them.
func TestEveryQueryParses(t *testing.T) {
	t.Parallel()

	built := map[string]string{
		"HTTPRequestRate":    queries.HTTPRequestRate(sel),
		"HTTPErrorRate":      queries.HTTPErrorRate(sel),
		"HTTPErrorRatio":     queries.HTTPErrorRatio(sel),
		"HTTPLatencyP95":     queries.HTTPLatencyQuantile(sel, 0.95),
		"HTTPLatencyP99":     queries.HTTPLatencyQuantile(sel, 0.99),
		"HTTPLatencyAverage": queries.HTTPLatencyAverage(sel),
		"Goroutines":         queries.Goroutines(sel),
		"HeapAllocBytes":     queries.HeapAllocBytes(sel),
		"GCTimeRatio":        queries.GCTimeRatio(sel),
		"CPUSeconds":         queries.CPUSeconds(sel),
		"Restarts":           queries.Restarts(sel),
		"Up":                 queries.Up(sel),
	}

	for name, expr := range built {
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

func TestQuantileQueryKeepsLeInTheInnerAggregation(t *testing.T) {
	t.Parallel()

	// Dropping `le` makes histogram_quantile return NaN with no error, which is
	// the most common way this query is written wrong.
	got := queries.HTTPLatencyQuantile(sel, 0.95)
	if !strings.Contains(got, "by (le, http_route)") {
		t.Errorf("HTTPLatencyQuantile() = %q, want `le` in the inner aggregation", got)
	}
}

func TestRatioQueriesGuardAgainstDivideByZero(t *testing.T) {
	t.Parallel()

	// Without the guard an idle service reports NaN, a threshold treats NaN as
	// "not breaching", and the alert silently never fires.
	for name, expr := range map[string]string{
		"HTTPErrorRatio":     queries.HTTPErrorRatio(sel),
		"HTTPLatencyAverage": queries.HTTPLatencyAverage(sel),
	} {
		if !strings.Contains(expr, "> 0)") {
			t.Errorf("%s = %q, want a `> 0` guard on the denominator", name, expr)
		}
	}
}

func TestRateQueriesUseTheRateIntervalMacro(t *testing.T) {
	t.Parallel()

	// A fixed window silently returns nothing once the panel's resolution is
	// coarser than the window.
	for name, expr := range map[string]string{
		"HTTPRequestRate": queries.HTTPRequestRate(sel),
		"CPUSeconds":      queries.CPUSeconds(sel),
		"GCTimeRatio":     queries.GCTimeRatio(sel),
	} {
		if !strings.Contains(expr, "[$__rate_interval]") {
			t.Errorf("%s = %q, want $__rate_interval", name, expr)
		}
	}
}
