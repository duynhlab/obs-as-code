# obs-as-code

Grafana dashboards as Go code, built with the
[Grafana Foundation SDK](https://github.com/grafana/grafana-foundation-sdk).
The repository emits Grafana Dashboard V2 resources and deployable
`GrafanaManifest` resources as JSON, then publishes the generated tree as an OCI
artifact for Flux.

```text
Go builders → Dashboard V2 JSON → GrafanaManifest JSON → OCI → Flux → Operator → Grafana
```

There is deliberately no generated Kubernetes YAML and no per-dashboard
`GrafanaDashboard.spec.oci` fetch. Flux pulls each release once and applies the
artifact-local Kustomization; Grafana Operator reconciles the inline App
Platform resources.

## Current scope

- `kubernetes-cluster-overview` — cluster health, capacity, nodes and networking
- `kubernetes-workloads` — namespace/workload/pod drill-down
- `obs-as-code-example` — build and conformance fixture; included in raw output,
  never deployed

The SDK is pinned exactly because its Dashboard V2 packages are public preview.
An upgrade is accepted only with reviewed golden-file changes.

## Requirements

- Go 1.26.7; `make` pins the toolchain used by CI
- `jq` and `kustomize` for offline artifact validation
- Docker for local preview
- `kubectl` plus the homelab context for server-side dry-run

## Common commands

| Command | Purpose |
|:--|:--|
| `make check` | Format, vet, lint, test, generate, validate, and detect drift |
| `make generate` | Rebuild every file under `generated/` |
| `make golden` | Accept an intentional rendering change for review |
| `make validate` | Check JSON contracts and build the artifact Kustomization |
| `make dry-run` | Validate the deployable resources against the live CRDs |
| `make preview` | Start Grafana 13.2 and create the V2 resources through its API |

`generated/` is committed build output. Never edit it manually.

## Repository shape

```text
cmd/generate                 deterministic artifact writer
internal/profile             datasource/plugin facts only
internal/queries/prometheus  PromQL, independent of Grafana types
internal/panels              reusable Dashboard V2 panel factories
internal/common              house defaults and layout composition
internal/dashboards/*        small domain-owned dashboard packages
internal/registry            explicit immutable catalog
internal/delivery            Dashboard/Folder → GrafanaManifest JSON
internal/check               artifact-level conformance rules
internal/catalog             one explicit aggregation point
generated/cluster/dashboards raw Dashboard V2 resources
generated/cluster/manifests  Flux-applied GrafanaManifest resources
```

The explicit catalog avoids import-side-effect registration. A package exports
`Dashboards()`, and `internal/catalog` composes the complete immutable registry.
This stays predictable as the number of dashboard packages grows.

## Add a dashboard

1. Copy `internal/dashboards/example/obs-as-code-example.go` into a domain
   package.
2. Put new expressions in `internal/queries/prometheus/`; do not inline PromQL.
3. Use `common.NewDashboard`, stable element keys, `.Span()` and `.Height()`.
4. Return the entry from the package's `Dashboards()` function.
5. Import and aggregate the package in `internal/catalog/catalog.go`.
6. Run `make golden && make check`, then inspect both golden and generated diffs.

Set `Delivery` only for dashboards that should become `GrafanaManifest`
resources. `Publish: false` keeps a fixture in conformance tests without placing
it in the OCI artifact.

## Delivery and compatibility

The outer Kubernetes API remains
`grafana.integreatly.org/v1beta1/GrafanaManifest`; the inner resource is
`dashboard.grafana.app/v2/Dashboard`. Operator 5.25.0 is the minimum selected
version because it fixes updates of V2 resources that require
`metadata.resourceVersion`.

See [architecture](docs/architecture.md), [Flux delivery](docs/flux/README.md),
[stack facts](docs/stack-facts.md), and [AGENTS.md](AGENTS.md) for the complete
contracts.
