# Flux delivery

The release workflow publishes `generated/` to
`oci://ghcr.io/duynhlab/obs-as-code:<tag>`. Homelab declares one
`OCIRepository` for that package and one Flux `Kustomization` with:

```yaml
spec:
  sourceRef:
    kind: OCIRepository
    name: obs-as-code-oci
  path: ./cluster/manifests
  targetNamespace: monitoring
  prune: true
```

The artifact path contains an extensionless, JSON-formatted `Kustomization`.
It applies one folder and the production dashboards as
`grafana.integreatly.org/v1beta1/GrafanaManifest` resources. Their inline
templates use `dashboard.grafana.app/v2` and `folder.grafana.app/v1`.
The homelab Kustomization defines CEL health expressions for
`ManifestSynchronized`; GrafanaManifest does not expose the conventional
`Ready` condition that Flux's generic kstatus check expects.

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
