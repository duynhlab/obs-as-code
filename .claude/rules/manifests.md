---
paths:
  - "internal/render/**/*.go"
  - "internal/check/**/*.go"
  - "flux/**"
  - "generated/**"
---

# Resources, rendering and delivery

## generated/ is output

Never hand-edit it. `make generate` writes every file and deletes any it did not
write, so a manual change disappears on the next run — after possibly having
been applied to a cluster in between.

It *is* committed, so a pull request shows the model change beside the Go change.
The resource embeds the model as a YAML block scalar under `spec.json`, so the
resource *is* the reviewable artifact — there is no second copy to drift.

`make preview` needs loose JSON files for Grafana's file provisioning, so
`generate -models <dir>` writes them on request into a gitignored directory.
Never commit those.

## Hand-written CR structs

`internal/render` declares its own structs instead of importing
`grafana-operator/v5/api/v1beta1`, which would pull in apimachinery and
controller-runtime for a handful of fields.

The cost is drift from the CRDs, so **`make validate` is not optional**:
`kubeconform -strict` against the real CRD OpenAPI schemas, deliberately without
`-ignore-missing-schemas` — every kind emitted here has a schema, so a missing
one means the catalog moved and the check has quietly stopped checking.

## CRD facts worth not rediscovering

- `spec.uid` is **immutable** on every kind. Renaming means delete then
  recreate.
- `spec.instanceSelector` is **required** and has no default. A resource that
  matches no instance is skipped in silence.
- The content fields — `json`, `gzipJson`, `url`, `jsonnet`, `configMapRef`,
  `grafanaCom`, `oci` — are mutually exclusive, and so are `folder`,
  `folderUID` and `folderRef`. This repo uses `gzipJson` and `folderRef`.
- `gzipJson` exists and is **not** used. It is gzip then base64, and gzip output
  is not byte-stable across Go releases: `compress/flate` produced different
  bytes under Go 1.26.7 and 1.27.0 for identical input, which broke `make diff`
  on any toolchain mismatch. `spec.json` is stable and readable.
- `spec.oci` exists as of operator 5.24.0 and keeps a model out of etcd
  entirely. That is the escape hatch if a board ever exceeds the size budget —
  splitting the board is the better first answer.
- `GrafanaAlertRuleGroup.spec.rules` requires at least one rule and
  `spec.interval` is required.

## Determinism

Output is committed, so anything non-reproducible shows up as a phantom diff on
an unrelated pull request — or, worse, as a CI failure on someone else's change.

Three things are pinned for this:

- `GOTOOLCHAIN` in the Makefile, because output must not depend on which Go the
  author happens to have installed
- the sort in `Selector.Matchers`, because map iteration order is randomised
- storing the model uncompressed, because a compressor's output is an
  implementation detail that changes between releases

If you add anything that iterates a map on the way to output, sort it. If you
add anything whose bytes come from a library rather than from your own code, ask
whether that library promises stability — `encoding/json` does, `compress/flate`
does not.

## Adding a check

A rule needs a test that feeds it a **deliberately broken** input and asserts it
fires. A rule that cannot fire is worse than no rule: it reports success forever
and nobody looks again.

Name the rule, export the identifier, and put the consequence in the message.
"panel has no query" is a fact; "panel has no query, so it renders empty" tells
the reader why they should care.

## Delivery

Tag → `release.yaml` → OCI artifact on GHCR → homelab `OCIRepository` pinned by
semver → `Kustomization` → operator → Grafana. Flux in that cluster consumes only
OCIRepositories; there is no `GitRepository` to point at. `flux/` holds the two
manifests homelab needs.
