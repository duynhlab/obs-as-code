#!/usr/bin/env bash
set -euo pipefail

out=${1:-generated}
dashboard_dir="$out/cluster/dashboards"
manifest_dir="$out/cluster/manifests"

for file in "$dashboard_dir"/*.json; do
  jq -e '
    .apiVersion == "dashboard.grafana.app/v2" and
    .kind == "Dashboard" and
    (.metadata.name | type == "string") and
    (.spec.elements | type == "object")
  ' "$file" >/dev/null
done

for file in "$manifest_dir"/*.json; do
  jq -e '
    .apiVersion == "grafana.integreatly.org/v1beta1" and
    .kind == "GrafanaManifest" and
    .metadata.namespace == "monitoring" and
    (.spec.instanceSelector.matchLabels.dashboards == "grafana") and
    (.spec.template.apiVersion | endswith("grafana.app/v1") or . == "dashboard.grafana.app/v2")
  ' "$file" >/dev/null
done

kustomize build "$manifest_dir" >/dev/null
echo "✔ generated JSON and Kustomization are valid"
