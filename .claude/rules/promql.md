---
paths:
  - "internal/queries/**/*.go"
  - "internal/promql/**/*.go"
---

# PromQL in this repo

## Where queries live

One exported function per expression, in a file named after the *signal*, not
after the board that first needed it: `http.go`, `runtime.go`, `pgx.go`,
`envoy.go`, `temporal.go`. Split by board and the same `rate()` gets copied
thirty times and drifts; split by signal and it gets reused.

Take a `Selector` and return a `string`. Never take or return a Grafana type —
`depguard` enforces that, and the reason is that the same function must be
callable from an alert rule as from a panel.

## Metric naming

RFC-0014: OpenTelemetry semconv names as VictoriaMetrics renders them with
`usePrometheusNaming`.

| semconv | PromQL |
|:--|:--|
| `http.server.request.duration` | `http_server_request_duration_seconds_{count,sum,bucket}` |
| `http.response.status_code` (attribute) | `http_response_status_code` (label) |
| `http.route` (attribute) | `http_route` (label) |

RFC-0017: database metrics come from `pgx.*`, so `pgx_query_duration_seconds_*`
— **not** semconv's `db.query.*`. `check.RuleDBNamespace` rejects the latter.

## Both parsers must accept it

`promql.Check` runs the expression through `VictoriaMetrics/metricsql` and
`prometheus/promql`. The first says the cluster's engine can run it; the second
says a board pointed at a plain Prometheus still works. So:

- **no MetricsQL-only syntax** — `keep_metric_names`, `WITH (...)`, `default`,
  `rollup_*` are all valid MetricsQL and invalid PromQL
- no experimental Prometheus functions either; the portable parser runs with
  every `Options` field false

## Variables and macros

A Grafana variable may appear **only inside a label matcher value**:

    up{job=~"$job"}          ✓
    topk($limit, up)         ✗ rejected
    sum by ($group) (up)     ✗ rejected

The reason is that nothing can substitute correctly in the rejected positions —
one needs a number, one a label name. Constraining the position makes the check
complete rather than a guess. If a board needs a variable elsewhere, resolve it
in Go and pass the resulting string.

PromQL's `$1` in a `label_replace` replacement is a regex capture reference,
not a Grafana variable, and remains valid. A Grafana `$name` in that same string
is rejected.

`Selector.Matchers()` requires a non-empty `Job`, and workload query functions
require a non-empty namespace. They panic on invalid programmer input so an
unscoped query cannot silently reach production.

Macros are substituted before parsing and must come from the list in
`internal/promql/promql.go`. A macro not in that list is rejected, so adding one
is a deliberate decision.

## Correctness traps

- **`rate()` before aggregation, always.** `rate(sum(x))` is wrong and reports a
  plausible-looking number.
- **Keep `le` in the inner aggregation** of `histogram_quantile`. Dropping it
  returns NaN with no error.
- **Guard every denominator with `> 0`.** Without it an idle service divides by
  zero, reports NaN, a threshold reads NaN as "not breaching", and the alert
  silently never fires.
- **`$__rate_interval`, never a fixed window.** A fixed window returns nothing
  once the panel is zoomed out past it.
