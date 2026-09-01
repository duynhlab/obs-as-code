# Delivering these dashboards

This repository emits **plain Grafana dashboard JSON** and nothing else. What
consumes it is deliberately not decided here — the same file imports into a
laptop's Grafana, Grafana Cloud, or the homelab cluster.

For homelab, the path is:

    tag v*  →  release.yaml  →  ghcr.io/duynhlab/obs-as-code:v*  →  GrafanaDashboard spec.oci  →  grafana-operator  →  Grafana

The operator fetches the file from the artifact itself, so the dashboard bytes
never enter etcd and no JSON is copied into another repository.

`spec.oci` requires grafana-operator **≥ 5.24.0**. The cluster runs 5.24.0.

## One resource per board

```yaml
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: kubernetes-cluster-overview
  labels:
    app.kubernetes.io/managed-by: obs-as-code
spec:
  instanceSelector:
    matchLabels:
      dashboards: grafana
  folder: "Platform / Infrastructure"
  allowCrossNamespaceImport: true
  resyncPeriod: 30s
  oci:
    reference: ghcr.io/duynhlab/obs-as-code:v0.2.0
    path: dashboards/kubernetes-cluster-overview.json
```

`path` must match the generated filename, which is always
`dashboards/<uid>.json`. That coupling is deliberate: renaming a board's uid
makes the operator's fetch **fail and say so in the resource's status**, rather
than leaving a stale board quietly serving old panels.

No `pullSecretRef`: the repository is public, so the package is too and the
operator pulls anonymously.

## Bumping a release

`spec.oci.reference` takes a tag or digest — the CRD's pattern is
`^[^:@]+(:[^:@/]+|@sha256:…)$`, so there is no semver range as Flux's
`OCIRepository` has. Left alone, that means editing every board's resource on
every release.

One patch in the enclosing `kustomization.yaml` avoids that, so a bump is one
line however many boards there are:

```yaml
patches:
  - target:
      kind: GrafanaDashboard
      labelSelector: app.kubernetes.io/managed-by=obs-as-code
    patch: |
      - op: replace
        path: /spec/oci/reference
        value: ghcr.io/duynhlab/obs-as-code:v0.2.0
```

Prefer a digest over a tag where reproducibility matters; the CRD accepts both.

## Why not an OCIRepository

An earlier version of this repo published Grafana Operator resources and had
homelab consume them through a Flux `OCIRepository` and `Kustomization`. That
worked, and it was the wrong shape: it made the output specific to one cluster
running one operator, and roughly 40% of the code existed to produce manifests
rather than dashboards.

The trade is real and worth knowing. Flux no longer tracks the version — the
reference above does — and a board's uid and its folder now live in two
repositories with nothing binding them but the `path` coupling described above.
