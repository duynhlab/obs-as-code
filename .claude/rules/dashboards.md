---
paths:
  - "internal/dashboards/**/*.go"
  - "internal/panels/**/*.go"
  - "internal/common/**/*.go"
  - "internal/registry/**/*.go"
---

# Dashboard V2 authoring

Dashboard packages export `Dashboards() []registry.Dashboard`. Registration is
explicit in `internal/catalog`; never use `init()` or a mutable global registry.

Start every board with `common.NewDashboard`. It owns tags, datasource variable,
time defaults, source link and the V2 `elements`/`layout` relationship.

Add panels with a stable kebab-case element key:

```go
board.Row("Traffic").Panel("request-rate", requestRate(p))
```

Size panels with `.Span()` and `.Height()`. Dashboard packages must not assemble
`GridLayout`, `RowsLayout`, `Element` or numeric panel IDs themselves.

PromQL comes from `internal/queries`; datasource references come from the
profile. Panel descriptions explain a non-obvious interpretation or failure
mode, not the title again.

`Publish` controls raw artifact output. `Delivery` separately opts a published
board into a production `GrafanaManifest`. The example is checked and golden
tested but has no `Delivery`.
