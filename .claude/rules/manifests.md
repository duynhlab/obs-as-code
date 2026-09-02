---
paths:
  - "internal/delivery/**/*.go"
  - "internal/render/**/*.go"
  - "internal/check/**/*.go"
  - "docs/flux/**"
  - "generated/**"
---

# JSON resources and delivery

`generated/` is committed output and is never hand-edited. Run `make generate`;
the generator owns `.json` files and the extensionless `Kustomization`.

The wire contract is fixed:

- raw: `dashboard.grafana.app/v2/Dashboard`
- deployable outer: `grafana.integreatly.org/v1beta1/GrafanaManifest`
- folder template: `folder.grafana.app/v1/Folder`
- all generated content is JSON, including `Kustomization`

Kubernetes delivery facts belong in `internal/delivery`, not `profile` or a
dashboard package. Flux pulls the OCI artifact and applies
`./cluster/manifests`; never generate `GrafanaDashboard.spec.oci` resources.

Conformance checks inspect rendered JSON. Every new rule needs a deliberately
broken fixture that proves it fires. Keep output deterministic: sort map-derived
lists and review golden changes after any SDK bump.

`make validate` is offline structural validation. `make dry-run` uses the live
CRDs. Neither proves controller update behavior; a V2 migration also needs a
create → update → delete canary on operator 5.25.0 or newer.
