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

## Registry authentication

`duynhlab/obs-as-code` is public, and so is its GHCR package, so Flux pulls
anonymously and no `secretRef` is needed. Generated resources carry metric
names, PromQL and panel titles — nothing that needs protecting.

If the repository is ever made private again, the package follows and this
OCIRepository starts failing with a 401. The fix then is a
`kubernetes.io/dockerconfigjson` secret through the ExternalSecrets/OpenBAO path
already in the cluster, referenced from `spec.secretRef`.
