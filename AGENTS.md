# AGENTS.md

Grafana dashboards and alert rules as Go code. `cmd/generate` renders them into
Grafana Operator custom resources, which a tag publishes as an OCI artifact that
Flux pulls into the homelab cluster.

`generated/` is build output. **Never edit it by hand** — `make generate` owns
every file there, and `make diff` fails if the tree and the code disagree.

## Commands

| command | when |
|:--|:--|
| `make check` | **before every commit.** fmt, vet, lint, test, generate, diff, validate |
| `make test` | tests only, with the race detector |
| `make generate` | re-render `generated/` after changing any resource |
| `make golden` | after an intended rendering change; then review `git diff internal/catalog/testdata` |
| `make validate` | kubeconform against the real CRD schemas |
| `make preview` | local Grafana with the rendered models, to look at a board |
| `make dry-run` | server-side dry-run against the current kube context |

## Layout

    cmd/generate            renders everything; flags only, logic lives in internal
    internal/profile        Profile — the only place a datasource UID or plugin type appears
    internal/folders        every Grafana folder, declared once
    internal/naming         the one identifier rule (DNS-1123 ∩ the CRDs' pattern)
    internal/queries/...    every PromQL string. No Grafana types here
    internal/panels         panel factories over a shared default
    internal/common         NewDashboard — every house default in one function
    internal/registry       Dashboard and AlertGroup; the Resource interface
    internal/render         Go models to CR YAML; gzip; the SDK-to-CRD translation
    internal/check          the conformance rules
    internal/catalog        imports every resource package; runs the conformance suite
    internal/dashboards/*   one file per board
    internal/alerts/*        one file per alert group
    docs/flux/              manifests for homelab to copy; not applied from here

## Rules

Each rule has a test or a lint rule behind it. If one blocks you, the code is
wrong, not the rule — raise it with a human rather than editing the rule.

1. **PromQL lives only in `internal/queries/**`.** A board or alert calls a
   function; it never writes a query inline. `depguard` keeps that package free
   of Grafana types so one expression can feed a panel *and* the alert about it.
2. **Never name a datasource UID or plugin type.** Use `profile.MetricsRef()`.
   Checked by `check.RuleDatasourceRef`.
3. **Rate windows use `$__rate_interval`**, never a literal `[5m]`. Checked by
   `check.RuleRateInterval`.
4. **Layout is `.Span()` and `.Height()`.** Never set `gridPos`. Checked by
   `check.RuleGridOverlap`.
5. **A Grafana variable may only appear inside a label matcher value**
   (`{job=~"$job"}`). Anywhere else no substitution can be correct, so the query
   checks reject it. See `internal/promql`.
6. **UIDs are kebab-case, ≤40 chars**, and equal the Go file's base name.
   Enforced by `internal/naming`; the filename part is on the reviewer.
7. **`spec.uid` is immutable in every Grafana Operator CRD.** Renaming a UID
   means deleting the resource first. Prefer not renaming.
8. **RFC-0017 — labels must be bounded.** No `user_id`, `order_id`,
   `session_id`, `payment_id`, `promo_code`, `request_id`, `trace_id`, `email`
   or IP in a matcher or a `by()` clause. Checked by
   `check.RuleForbiddenLabel`.
9. **RFC-0017 — database metrics are `pgx_*`**, not semconv's `db_query_*`.
   Checked by `check.RuleDBNamespace`.
10. **RFC-0017 — business metrics get their own board**, never mixed into a
    RED/runtime board. Reviewer-enforced; there is no check for it.
11. **Every board ships a golden file** in the same change. `make golden`.
12. **Every alert rule carries** a `severity` label and `summary`,
    `description` and `runbook_url` annotations.

## Adding a dashboard

Copy `internal/dashboards/example/obs-as-code-example.go`. Then:

1. Add any new query to `internal/queries/prometheus/`, split by signal.
2. Register in `init()` with `registry.Add(registry.Dashboard{...})` — data plus
   a `Build` function. Do not write methods; `Resource` is satisfied for you.
3. Add one import line to `internal/catalog/catalog.go`.
4. `make golden && make check`.

## Commits and pull requests

- **No attribution trailers.** No `Signed-off-by`, `Co-authored-by`,
  `Assisted-by`, `Generated-by`, or any AI or tool attribution.
- Subject ≤50 chars, capitalised, imperative, no trailing period — `Add X`, not
  `Added X.`
- Body for non-trivial changes: what and why, wrapped at 72, one blank line
  after the subject.
- No issue references (`Fixes #123`) and no @-mentions in commit messages; put
  those in the pull request description.
- A pull request must contain the regenerated `generated/**` beside the Go
  change. CI verifies it with `make diff`.

## Gotchas

- **The Foundation SDK is public preview** and reshapes types between releases.
  The version is pinned exactly. A bump is reviewed through the golden diff.
- **`dashboard.DashboardBuilder` is marked deprecated** in favour of
  `dashboardv2`. Ignore it: `GrafanaDashboard.spec.gzipJson` carries a classic
  model, and so does every board in the cluster. `.golangci.yml` excludes the
  warning and says why.
- **The SDK's alert model is not the CRD's.** `internal/render/alertrule.go`
  translates field by field — the SDK writes `notification_settings` where the
  CRD reads `notificationSettings`, carries `keepFiringFor` in nanoseconds where
  the CRD wants a duration string, and requires a `ruleGroup` the CRD rejects.
- **`make` pins `GOTOOLCHAIN` to the version CI uses.** `toolchain` in `go.mod`
  is only a floor, so a newer local Go would otherwise produce different output
  than the runner. Run things through `make`, not bare `go`.
- **Always `go mod tidy`, never `-mod=mod`.** `client_golang` imports `procfs`
  from a Linux-only file, so a macOS build never needs it and never records it
  in `go.sum` — CI then fails on a missing entry. `make tidy-check` and
  `make build-linux` are the two gates for that.
- **The model is stored uncompressed in `spec.json`.** `spec.gzipJson` was tried
  and abandoned: `compress/flate` output differs between Go 1.26.7 and 1.27.0,
  so a committed compressed blob broke `make diff` whenever a toolchain
  differed. Uncompressed, the resource is also the reviewable artifact.
- **The operator runs `watchNamespaces: monitoring`.** A resource in another
  namespace is ignored with no error reported.
- **Temporal:** `service_error_with_type` is the server error counter.
  `service_errors` does not exist.
- **Envoy job labels** are `envoy-gateway` (data plane) and
  `envoy-gateway-controller`. Panels key on those.
- **No Loki in this stack.** Logs are Vector → VictoriaLogs
  (`victoriametrics-logs-datasource`, uid `victorialogs`).

Longer reference — datasource UIDs, operator versions, the full stack — is in
`docs/stack-facts.md`. It is deliberately not imported here, so it costs no
context until someone needs it.
