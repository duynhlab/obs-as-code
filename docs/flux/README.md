# Delivering these dashboards

This repository emits **plain Grafana dashboard JSON** and nothing else. What
consumes it is deliberately not decided here — the same file imports into a
laptop's Grafana, Grafana Cloud, or the homelab cluster.

For homelab, the path is:

    tag v*  →  release.yaml  →  ghcr.io/duynhlab/obs-as-code:v*  →  GrafanaDashboard spec.oci  →  grafana-operator  →  Grafana

The operator fetches the file from the artifact itself, so no JSON is copied
into another repository and the resource spec stays a dozen lines.

One earlier claim here was wrong and is worth correcting precisely: the model
does not stay out of etcd. After every successful reconcile the operator writes
a gzipped copy of it into the resource's `status.contentCache`
(`dashboard_controller.go` calls `UpdateCache`, which caches URL, grafana.com
*and* OCI sources) — a cache that, for OCI, nothing ever reads back:
`GetContentCache` is consulted only by the URL and grafana.com fetchers. The
real benefits of `spec.oci` are the small declarative spec, the single source of
truth, and no cross-repo JSON copies — not etcd avoidance.

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
    reference: ghcr.io/duynhlab/obs-as-code:v0.2.2
    path: cluster/dashboards/kubernetes-cluster-overview.json
```

`path` must match the file's location inside the artifact, which is
`<profile>/dashboards/<uid>.json` — **including the profile directory**. Today
there is one profile, `cluster`, so every path starts `cluster/`. The profile
dimension exists so a second target (a local stack, a different backend) can be
rendered from the same boards without changing them.

To see the exact paths an artifact contains:

```console
$ flux pull artifact oci://ghcr.io/duynhlab/obs-as-code:latest --output /tmp/a
$ (cd /tmp/a && find . -name '*.json' | sed 's|^\./||')
cluster/dashboards/kubernetes-cluster-overview.json
cluster/dashboards/obs-as-code-example.json
```

Read the path from the artifact rather than transcribing it: a wrong `path` is a
fetch failure at reconcile time, and the resource's status is the only place it
shows.

The coupling to the uid is deliberate. Renaming a board makes the fetch **fail
and say so in the resource's status**, rather than leaving a stale board quietly
serving old panels.

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
        value: ghcr.io/duynhlab/obs-as-code:v0.2.2
```

Prefer a digest over a tag where reproducibility matters; the CRD accepts both.

## resyncPeriod is a fetch, every time

Read from operator 5.24.0 source, not assumed: the OCI fetcher has **no cache
read path** — `GetContentCache` is only consulted for `url:` and `grafanaCom:`
sources, so every resync of an OCI board is a full artifact pull. At the house
default of `resyncPeriod: 30s` that is ~2,880 GHCR pulls per day per board, for
an artifact that is immutable per tag.

The only thing resync buys an OCI board is drift-revert — the boards are
`editable` by design, and resync is what overwrites a UI edit. `10m` bounds that
at 144 pulls a day and reverts an edit inside ten minutes. Use `10m`, not the
`30s` the `url:` boards carry (those had `contentCacheDuration` making their
resyncs cheap; OCI has no equivalent knob).

## Reading the status

Two conditions look alike and are not:

- **`DashboardSynchronized=True / ApplySuccessful`** — fetched, applied, done.
- **`NoMatchingInstance=True / EmptyAPIReply`** — looks like a fetch failure and
  is not one. The operator does not evaluate the board until a `Grafana`
  instance matches the selector; on a cold cluster, Grafana can trail the
  Keycloak/ESO secret chain by ~10 minutes and every board sits in this state
  meanwhile. Wait for the instance before debugging the artifact.

A wrong `path` or `reference` shows up on `DashboardSynchronized` and nowhere
else.

## Verifying a board against the live cluster

From the live audit of the first board (homelab PR 963) — the checks that told
truth from plausible nonsense:

```bash
# The job labels every query keys on. A mismatch blanks whole rows silently.
count by (job) (kube_pod_info)                        # → kube-state-metrics
count by (job) (container_cpu_usage_seconds_total)    # → kubelet

# An empty count() panel: true negative or dead query? Check the base family.
count(kube_statefulset_status_replicas)               # >0 → empty was correct

# The exported_* trap, proved symmetrically:
count by (namespace) (kube_pod_info)             # 1 series, all kube-system — WRONG
count by (exported_namespace) (kube_pod_info)    # one per real namespace — RIGHT
```

One family stays unprovable on a fresh cluster:
`kube_pod_container_status_last_terminated_reason` is only emitted once a
container has terminated, so an empty OOMKilled panel cannot be distinguished
from a wrong metric name until then. Re-check after any Job completes.

## Why not an OCIRepository

An earlier version of this repo published Grafana Operator resources and had
homelab consume them through a Flux `OCIRepository` and `Kustomization`. That
worked, and it was the wrong shape: it made the output specific to one cluster
running one operator, and roughly 40% of the code existed to produce manifests
rather than dashboards.

The trade is real and worth knowing. Flux no longer tracks the version — the
reference above does — and a board's uid and its folder now live in two
repositories with nothing binding them but the `path` coupling described above.
