package promql_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/promql"
)

func TestSanitizeSubstitutesMacros(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want string
	}{
		{
			name: "rate interval",
			expr: `rate(http_requests_total[$__rate_interval])`,
			want: `rate(http_requests_total[5m])`,
		},
		{
			name: "interval",
			expr: `avg_over_time(up[$__interval])`,
			want: `avg_over_time(up[1m])`,
		},
		{
			// The longer macro must win, or "$__interval_ms" becomes "1ms".
			name: "interval_ms is not eaten by interval",
			expr: `up * $__interval_ms`,
			want: `up * 60000`,
		},
		{
			name: "range_ms is not eaten by range",
			expr: `up * $__range_ms`,
			want: `up * 3600000`,
		},
		{
			name: "range",
			expr: `increase(errors_total[$__range])`,
			want: `increase(errors_total[1h])`,
		},
		{
			name: "variable inside a matcher is left alone",
			expr: `up{job=~"$job"}`,
			want: `up{job=~"$job"}`,
		},
		{
			name: "braced variable inside a matcher is left alone",
			expr: `up{namespace=~"${ns}"}`,
			want: `up{namespace=~"${ns}"}`,
		},
		{
			name: "macro and variable together",
			expr: `sum(rate(http_requests_total{job=~"$job"}[$__rate_interval])) by (route)`,
			want: `sum(rate(http_requests_total{job=~"$job"}[5m])) by (route)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := promql.Sanitize(tt.expr)
			if err != nil {
				t.Fatalf("Sanitize(%q) error = %v", tt.expr, err)
			}
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestSanitizeRejectsVariableOutsideAMatcher(t *testing.T) {
	t.Parallel()

	// Each of these is a position where no substitution can be correct: one
	// needs a number, one a label name, one a metric name. Rejecting them is
	// what makes this check complete rather than a guess.
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "as a function argument", expr: `topk($limit, up)`, want: "$limit"},
		{name: "as a label name", expr: `sum by ($group) (up)`, want: "$group"},
		{name: "as a metric name", expr: `$metric{job="a"}`, want: "$metric"},
		{name: "braced outside a string", expr: `topk(${limit}, up)`, want: "${limit}"},
		{name: "after a closed matcher", expr: `up{job="a"} > $threshold`, want: "$threshold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := promql.Sanitize(tt.expr)
			if err == nil {
				t.Fatalf("Sanitize(%q) = nil error, want a rejection", tt.expr)
			}
			if !errors.Is(err, promql.ErrVariablePosition) {
				t.Errorf("Sanitize(%q) error does not wrap ErrVariablePosition: %v", tt.expr, err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Sanitize(%q) = %v, want it to name %q", tt.expr, err, tt.want)
			}
		})
	}
}

func TestSanitizeHandlesQuotingEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{name: "single quoted matcher", expr: `up{job=~'$job'}`},
		{name: "escaped quote before variable", expr: `up{msg=~"say \"hi\" to $job"}`},
		{name: "two matchers each with a variable", expr: `up{job=~"$job",ns=~"$ns"}`},
		{
			// The variable sits after the string closes, so it is outside.
			name:    "variable after a closing quote",
			expr:    `up{job=~"a"} + $x`,
			wantErr: true,
		},
		{
			// A dollar inside a string is fine even next to a quote.
			name: "dollar at end of string",
			expr: `up{job=~"$"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := promql.Sanitize(tt.expr)
			if tt.wantErr && err == nil {
				t.Errorf("Sanitize(%q) = nil error, want a rejection", tt.expr)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Sanitize(%q) error = %v, want nil", tt.expr, err)
			}
		})
	}
}

func TestCheckAcceptsRealisticQueries(t *testing.T) {
	t.Parallel()

	// These are the shapes this repo actually generates, including the RFC-0014
	// semconv-to-Prometheus naming and the divide-by-zero guard.
	exprs := []string{
		`sum(rate(http_server_request_duration_seconds_count{job=~"$job"}[$__rate_interval])) by (http_route)`,
		`histogram_quantile(0.95, sum(rate(http_server_request_duration_seconds_bucket{job=~"$job"}[$__rate_interval])) by (le, http_route))`,
		`sum(rate(http_server_request_duration_seconds_count{job=~"$job",http_response_status_code=~"5.."}[$__rate_interval])) / (sum(rate(http_server_request_duration_seconds_count{job=~"$job"}[$__rate_interval])) > 0)`,
		`sum(rate(pgx_query_duration_seconds_count{job=~"$job"}[$__rate_interval])) by (operation)`,
		`sum(rate(envoy_http_downstream_rq_xx{job="envoy-gateway",envoy_response_code_class="5"}[$__rate_interval]))`,
		`go_goroutines{job=~"$job"}`,
		`topk(10, sum(rate(temporal_workflow_completed{job=~"$job"}[$__rate_interval])) by (workflow_type))`,
		`absent(up{job=~"$job"})`,
		`changes(process_start_time_seconds{job=~"$job"}[$__range])`,
	}

	for _, expr := range exprs {
		t.Run(shortName(expr), func(t *testing.T) {
			t.Parallel()

			if err := promql.Check(expr); err != nil {
				t.Errorf("Check(%q) = %v", expr, err)
			}
		})
	}
}

func TestCheckRejectsBrokenQueries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
	}{
		{name: "empty", expr: ""},
		{name: "blank", expr: "   "},
		{name: "unclosed paren", expr: `sum(rate(up[5m])`},
		{name: "unclosed matcher", expr: `up{job="a"`},
		{name: "bad duration", expr: `rate(up[5x])`},
		{name: "unknown function", expr: `not_a_function(up)`},
		{name: "missing range on rate", expr: `rate(up)`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := promql.Check(tt.expr); err == nil {
				t.Errorf("Check(%q) = nil error, want a rejection", tt.expr)
			}
		})
	}
}

func TestValidatePortableRejectsMetricsQLOnlySyntax(t *testing.T) {
	t.Parallel()

	// This is the whole reason both parsers run. Each of these is valid
	// MetricsQL and invalid PromQL: a board using one works on the cluster and
	// breaks the moment it is pointed at a plain Prometheus datasource.
	tests := []struct {
		name string
		expr string
	}{
		{name: "keep_metric_names", expr: `rate(up[5m]) keep_metric_names`},
		{name: "WITH template", expr: `WITH (f(x) = x * 2) f(up)`},
		{name: "default operator", expr: `up default 0`},
		{name: "rollup function", expr: `rollup_rate(up[5m])`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// metricsql accepts it — if it stops doing so this test is
			// asserting nothing and should be revisited.
			if err := promql.Validate(tt.expr); err != nil {
				t.Skipf("metricsql no longer accepts %q: %v", tt.expr, err)
			}

			if err := promql.ValidatePortable(tt.expr); err == nil {
				t.Errorf("ValidatePortable(%q) = nil error, want it rejected as non-portable", tt.expr)
			}
			if err := promql.Check(tt.expr); err == nil {
				t.Errorf("Check(%q) = nil error, want it rejected", tt.expr)
			}
		})
	}
}

func shortName(expr string) string {
	if i := strings.IndexAny(expr, "({"); i > 0 {
		return expr[:i]
	}
	if len(expr) > 24 {
		return expr[:24]
	}
	return expr
}
