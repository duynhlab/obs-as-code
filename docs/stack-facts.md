# Stack facts

Reference for the cluster these resources target. Linked from `AGENTS.md` but
deliberately not imported there, so it costs no context until someone needs it.

Verified 2026-08-31 against `duynhlab/homelab`, `duynhlab/helm-charts` and the
grafana-operator v5.24.0 source.

## Grafana and the operator

| | |
|:--|:--|
| Grafana | `grafana/grafana:13.2.0`, `Grafana/grafana` in namespace `monitoring` |
| Operator | chart pinned `5.24.0` via `OCIRepository grafana-operator-oci` |
| API group | `grafana.integreatly.org/v1beta1` (no v1beta2 anywhere) |
| Watched namespaces | `monitoring` only — a resource elsewhere is ignored silently |
| Instance selector | `matchLabels: {dashboards: grafana}` |
| Existing resources use | `resyncPeriod: 30s`, `allowCrossNamespaceImport: true` |
| Auth | Keycloak OIDC, `infra-team`→Admin, `sre-team`→Editor, else Viewer |

## Datasources

Three VictoriaMetrics datasources, one backend at
`http://vmsingle-victoria-metrics.monitoring.svc:8428`.

| name | type | uid | role |
|:--|:--|:--|:--|
| `VictoriaMetrics` | `victoriametrics-metrics-datasource` | `victoriametrics` | `isDefault`, plugin 0.25.2 |
| `VictoriaMetrics (Prometheus)` | `prometheus` | `victoriametrics-prometheus` | `manageAlerts: true`; the ruler API rejects non-prometheus types |
| `Prometheus` | `prometheus` | `prometheus` | claims the literal uid that upstream boards hardcode |

`profile.Cluster()` targets the **prometheus** plugin type, not the VM plugin.
Two prometheus-type aliases already front the same backend, so a board built
that way runs on the cluster, on the local stack, and against a real Prometheus
unchanged.

**Local-stack divergence:** `homelab/local-stack/.../datasources` defines the
same uid `victoriametrics` with `type: prometheus`. Same uid, different type.
This is why the plugin type is a `Profile` field.

Other datasources: `victorialogs` (`victoriametrics-logs-datasource`, `:9428`),
`victoriatraces` (`type: jaeger`), `pyroscope`
(`grafana-pyroscope-datasource`, `:4040`), `clickhouse`
(`grafana-clickhouse-datasource`, db `otel`). **No Loki** — logging is Vector →
VictoriaLogs. Tempo and Jaeger datasources were retired by RFC-0027.

## Delivery, today

Dashboards reach Grafana through three parallel channels, which this repo exists
to collapse:

| channel | count |
|:--|:--|
| `helm-charts` chart → ConfigMap → `configMapRef` | 2 |
| in-repo JSON → `configMapGenerator` → `configMapRef` | 22 |
| `url:` fetched at reconcile | 15 |

Seven of those URLs point at `duynhlab/grafana-dashboards@main` — an unpinned
branch in a repo whose own README says not to add dashboards to it.

Flux consumes **only** OCIRepositories. Existing sources:
`grafana-operator-oci`, `grafana-dashboards-chart-oci`, `infrastructure-oci`.
There is no `GitRepository` in the cluster.

## Alerting, today

73 `PrometheusRule` files, roughly 183 rule groups, evaluated by vmalert. Grafana
sees them read-only through the `prometheus` alias with `manageAlerts: true`.

There are **no** `GrafanaAlertRuleGroup`, `GrafanaContactPoint` or
`GrafanaNotificationPolicy` resources anywhere. Grafana-managed alerting is
greenfield, and whether to adopt it is an open decision — RFC-0017 explicitly
deferred business-metric alerts and SLOs.

## Folders

No `GrafanaFolder` resources exist. Folders are implicit strings duplicated
between the cluster resources and the local-stack provisioning, and they have
already drifted once. `internal/folders` is the fix.

## Metric naming

RFC-0014 (implemented): OTLP push for metrics, logs and traces; semconv names
adopted directly, rendered by VictoriaMetrics with `usePrometheusNaming`.

RFC-0017 (implemented, closed 2026-07-16) is normative:

- three-layer signal ownership — `web/v1` transport RED (automatic, never
  hand-written), `logic/v1` business metrics, `core` DB and cache
- **bounded labels are a hard rule.** `user_id`, `order_id`, `session_id`,
  `payment_id`, `promo_code`, IPs and `err.Error()` are forbidden as labels or
  span attributes. The SDK caps at 2000 attribute sets per metric and then
  overflows silently
- **database metrics are `pgx.*`**, not semconv `db.query.*`
- **business metrics get their own `$app`-templated board**, never mixed into a
  RED/runtime board

## Workloads worth knowing

- **Envoy Gateway** — data plane scraped at `:19001/stats/prometheus`, relabelled
  to `job: envoy-gateway`; control plane to `job: envoy-gateway-controller`.
  Those two job labels are the stable selectors.
- **Temporal** — server 1.31.2, official chart 1.6.0. `service_error_with_type`
  is the server error counter; `service_errors` does not exist.
- **CloudNativePG** with PgDog and PgBouncer.
- Also: VictoriaMetrics/VMAgent/VMAlert + Karma, VictoriaLogs, VictoriaTraces,
  ClickHouse, Pyroscope, Keycloak, Sloth, Kyverno, cert-manager, ESO/OpenBAO.

## RFC and ADR process

`homelab/docs/proposals/rfc/RFC-0000` … `RFC-0028` (no RFC-0016), and
`docs/proposals/adr/ADR-000` … `ADR-054`. RFC-0000 is the template.

No RFC covers this repo yet. One is planned but deliberately not a blocker.
