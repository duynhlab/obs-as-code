# Flux delivery

The release workflow publishes `generated/` to
`oci://ghcr.io/duynhlab/obs-as-code:<tag>`. Homelab declares one
`OCIRepository` for that package and **two** Flux `Kustomization`s — folders
first, dashboards depending on them:

```yaml
# obs-as-code-folders
spec:
  sourceRef:
    kind: OCIRepository
    name: obs-as-code-oci
  path: ./cluster/manifests/folders
  targetNamespace: monitoring
  prune: true
---
# obs-as-code-dashboards
spec:
  sourceRef:
    kind: OCIRepository
    name: obs-as-code-oci
  path: ./cluster/manifests/dashboards
  targetNamespace: monitoring
  prune: true
  dependsOn:
    - name: obs-as-code-folders   # see below; not optional
```

Each path contains an extensionless, JSON-formatted `Kustomization`, and
nothing sits directly under `manifests/` — a file there would belong to
neither wave. Both apply
`grafana.integreatly.org/v1beta1/GrafanaManifest` resources whose inline
templates use `dashboard.grafana.app/v2` and `folder.grafana.app/v1`.
The homelab Kustomizations define CEL health expressions for
`ManifestSynchronized`; GrafanaManifest does not expose the conventional
`Ready` condition that Flux's generic kstatus check expects.

## Why the split is two waves and not one directory

The Grafana Operator applies each `GrafanaManifest` independently, in no
guaranteed order. A dashboard whose `grafana.app/folder` annotation names a
folder that has not been created yet fails outright:

```
applying resource: creating resource:
folders.folder.grafana.app "platform-infrastructure" not found
```

Measured on a cold bring-up while both lived in one directory: the folder
applied, both dashboards failed, and the wave reported `HealthCheckFailed`
for six minutes before the operator's next resync repaired it. The wave
could not simply wait it out either — the operator's `resyncPeriod` is 10m
while the wave's `timeout` is 5m, so the retry always arrives after the
deadline.

Ordering *within* one Kustomization guarantees nothing. Only `dependsOn`
between two of them does, and two Kustomizations need two paths — which is
why the artifact ships the split rather than leaving homelab to slice one
directory.

## Why this replaced `GrafanaDashboard.spec.oci`

With one `spec.oci` resource per board, Grafana Operator independently fetched
the same immutable artifact on each resource's resync. The operator's OCI path
does not read its content cache, so dashboard count multiplied registry pulls.

Flux now owns artifact acquisition and revision status. It pulls once, builds
the committed manifest set, applies it, prunes removed resources, and exposes a
single revision in status. Grafana Operator only reconciles inline App Platform
resources.

## Release sequence

1. Run `make golden && make check` and review `generated/**`.
2. Tag the obs-as-code repository; CI publishes the matching OCI tag.
3. Bump the homelab `OCIRepository.spec.ref.semver` to that version.
4. Reconcile the source and Kustomization.
5. Confirm all `GrafanaManifest` health checks and inspect both dashboards.

Do not point homelab at a release tag before the artifact exists. The planned
cutover version for this migration is `v0.4.0`.

## Verification

```bash
flux get source oci obs-as-code-oci -n flux-system
flux get kustomization obs-as-code-dashboards-local -n flux-system
kubectl get grafanamanifest -n monitoring
```

Use `make dry-run` before release. A dry-run proves CRD admission but not the
operator's create/update behavior; that requires a canary on operator 5.25.0 or
newer.
