// Package prometheus holds every PromQL expression this repo generates.
//
// Nothing else may contain a query string. A dashboard or alert file calls a
// function here; it never writes PromQL inline. Two reasons:
//
//   - the same expression then feeds a panel and an alert rule, so a graph and
//     the alert about it cannot disagree
//   - every expression is reachable from one place, which is what makes the
//     parse and label-cardinality checks exhaustive rather than best-effort
//
// The package imports no Grafana SDK, and .golangci.yml enforces that with
// depguard: an expression that knows about panels is an expression that cannot
// be reused by an alert.
//
// Metric names follow RFC-0014: OpenTelemetry semconv names as VictoriaMetrics
// renders them with usePrometheusNaming, so http.server.request.duration
// becomes http_server_request_duration_seconds_*. Database metrics follow
// RFC-0017 and live in the pgx.* namespace, not semconv's db.query.*.
package prometheus

import (
	"fmt"
	"slices"
	"strings"
)

// Selector is the label matcher set shared by every query.
//
// Fields hold Grafana variable references such as "$job", which land inside
// matcher values where a PromQL parser accepts them. Building matchers here
// rather than in each query means a label-scheme change is one edit.
type Selector struct {
	// Job matches the scrape job. Required: an unscoped query aggregates across
	// every service in the cluster, which is never what a board wants.
	Job string

	// Namespace optionally matches the Kubernetes namespace.
	Namespace string

	// Extra holds additional equality matchers, applied in sorted key order so
	// the rendered expression is stable.
	Extra map[string]string
}

// Matchers renders s as a PromQL label matcher list including the braces, e.g.
// `{job=~"$job",namespace=~"$ns"}`.
func (s Selector) Matchers(additional ...string) string {
	if err := s.Validate(); err != nil {
		panic(err)
	}
	parts := make([]string, 0, 2+len(s.Extra)+len(additional))

	if s.Job != "" {
		parts = append(parts, fmt.Sprintf("job=~%q", s.Job))
	}
	if s.Namespace != "" {
		parts = append(parts, fmt.Sprintf("namespace=~%q", s.Namespace))
	}

	keys := make([]string, 0, len(s.Extra))
	for k := range s.Extra {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=~%q", k, s.Extra[k]))
	}

	parts = append(parts, additional...)

	return "{" + strings.Join(parts, ",") + "}"
}

// Validate reports whether s is scoped enough to be safe.
func (s Selector) Validate() error {
	if strings.TrimSpace(s.Job) == "" {
		return fmt.Errorf("selector: job is empty; an unscoped query aggregates across the whole cluster")
	}
	return nil
}

// rateInterval is Grafana's own rate window macro. Using it rather than a fixed
// window means the query adapts to the panel's resolution and to the scrape
// interval, instead of silently returning nothing when someone zooms out.
const rateInterval = "$__rate_interval"
