# Flux wiring

These manifests belong in the **homelab** repository, not here. They are kept in
this repo so the delivery path lives beside the thing being delivered, and so a
change to the artifact and a change to how it is consumed can be reviewed
together.

Applying them is a separate homelab pull request.

## How this reaches the cluster

    tag v*  →  release.yaml  →  ghcr.io/duynhlab/obs-as-code:v*  →  OCIRepository  →  Kustomization  →  grafana-operator  →  Grafana

Flux in homelab consumes **only** OCIRepositories — there is no `GitRepository`
anywhere in that cluster — so an OCI artifact is the path that fits, not a new
mechanism. `grafana-operator-oci` and `grafana-dashboards-chart-oci` are wired
the same way.

## Install

Copy the two files into homelab:

| file | destination |
|:--|:--|
| `ocirepository.yaml` | `kubernetes/clusters/local/sources/oci/obs-as-code-oci.yaml` |
| `kustomization.yaml` | `kubernetes/clusters/local/obs-as-code.yaml` |

Then add the source to `kubernetes/clusters/local/sources/kustomization.yaml`
and the Kustomization to the cluster's resource list.

## Open item — registry authentication

`duynhlab/obs-as-code` is a **private** repository, so its GHCR package is
private too and Flux needs `spec.secretRef` pointing at a
`kubernetes.io/dockerconfigjson` secret. The three OCI sources already in the
cluster all pull public packages, so there is no existing secret to reuse.

Two ways forward:

1. **Make the GHCR package public.** Generated dashboards contain no secrets —
   metric names, PromQL, and panel titles. This is the recommended option and
   needs no cluster change.
2. Provision a pull secret through the ExternalSecrets/OpenBAO path already in
   the cluster, and add `secretRef` to the OCIRepository.

Until one is chosen, the OCIRepository below will fail to pull with a 401.
