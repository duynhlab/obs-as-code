# AGENTS.md

Grafana dashboards as Go code. `cmd/generate` emits raw Dashboard V2 resources
and deployable GrafanaManifest resources as JSON. A tag publishes `generated/`
as OCI; Flux pulls it once and applies its Kustomization.

`generated/` is owned by `make generate`. Never edit it manually.

## Commands

| Command | When |
|:--|:--|
| `make check` | Before every commit: tidy, fmt, vet, Linux build, lint, test, generate, validate, diff |
| `make generate` | After changing a dashboard, registry, profile, panel or delivery code |
| `make golden` | After an intended rendering change; review the golden diff |
| `make validate` | Offline JSON contract and Kustomization validation |
| `make dry-run` | Server-side admission against the current Kubernetes context |
| `make preview` | Create the generated V2 resources in local Grafana |

## Layout

```text
cmd/generate                 deterministic writer only
internal/profile             datasource/plugin facts; no Kubernetes delivery
internal/queries/prometheus  all PromQL; no Grafana types
internal/panels              Dashboard V2 panel factories
internal/common              defaults and elements/layout composition
internal/dashboards/*        domain packages exporting Dashboards()
internal/registry            immutable explicit catalog entries
internal/delivery            GrafanaManifest and folder wrappers
internal/check               rendered-JSON conformance rules
internal/catalog             aggregates every dashboard package once
generated/cluster/dashboards raw dashboard.grafana.app/v2 resources
generated/cluster/manifests  Flux-applied GrafanaManifest JSON
```

## Rules

1. PromQL lives only in `internal/queries/**`; dashboard code calls functions.
2. Datasources come from `profile.MetricsRef()` and
   `profile.MetricsVariable()`; never hardcode a UID or plugin in a board.
3. Rate windows use `$__rate_interval`, never a literal range.
4. Layout uses stable element keys plus `.Span()` and `.Height()`; never build
   V2 `elements` and `layout` separately in a dashboard package.
5. Grafana variables may occur only inside label matcher values. PromQL regex
   replacement `$1` is not a Grafana variable.
6. A `Selector` requires `Job`; a `WorkloadSelector` requires `Namespace`.
   Invalid selectors fail fast rather than produce fleet-wide queries.
7. UIDs are kebab-case, at most 40 characters, and match the Go filename.
8. RFC-0017 forbids unbounded identity labels in matchers or aggregations and
   requires `pgx_*`, not `db_query_*`, for database metrics.
9. Business metrics belong on their own dashboard, not RED/runtime boards.
10. Every dashboard has a golden file in the same change.
11. Production dashboards set both `Publish: true` and `Delivery`; examples may
    be checked without deployment by omitting `Delivery`.
12. The generated Kubernetes kind is `GrafanaManifest`, never
    `GrafanaDashboard`; generated content is JSON, never YAML.

Each enforceable rule has a negative test proving it fires. Do not weaken a
rule to land a dashboard.

## Adding a dashboard

1. Copy `internal/dashboards/example/obs-as-code-example.go` into a domain
   package and add query functions by signal.
2. Return `registry.Dashboard` values from the package's `Dashboards()`.
3. Aggregate that slice in `internal/catalog/catalog.go`; do not use `init()` or
   global registration.
4. Run `make golden && make check`; inspect golden and generated diffs.

## Delivery contract

- Outer: `grafana.integreatly.org/v1beta1`, kind `GrafanaManifest`.
- Inner dashboard: `dashboard.grafana.app/v2`, kind `Dashboard`.
- Inner folder: `folder.grafana.app/v1`, kind `Folder`.
- Namespace: `monitoring`; instance selector: `dashboards=grafana`.
- Flux path: `./cluster/manifests` in the OCI artifact.
- Operator: 5.25.0 or newer. 5.24.0 cannot reliably update V2 resources because
  it omits the required `resourceVersion`.

## Git

- No attribution trailers.
- Commit subject: imperative, capitalised, no period, at most 50 characters.
- Commit generated output with its Go source. CI rejects drift.
- Never tag or push a release unless explicitly requested.

## Gotchas

- Dashboard V2 and the Foundation SDK are public preview. Pin the SDK exactly
  and review every golden diff on bump.
- Dashboard identity now lives in resource `metadata.name`, not classic
  `spec.uid`.
- `make preview` must use the Grafana V2 API; file provisioning expects classic
  dashboard JSON and is not a valid V2 preview.
- The operator watches `monitoring` only.
- No Loki: logs are Vector → VictoriaLogs.

See `docs/architecture.md`, `docs/flux/README.md`, and `docs/stack-facts.md`.
