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

for file in "$manifest_dir"/*/*.json; do
  jq -e '
    .apiVersion == "grafana.integreatly.org/v1beta1" and
    .kind == "GrafanaManifest" and
    .metadata.namespace == "monitoring" and
    (.spec.instanceSelector.matchLabels.dashboards == "grafana") and
    (.spec.template.apiVersion | endswith("grafana.app/v1") or . == "dashboard.grafana.app/v2")
  ' "$file" >/dev/null
done

# Folders and dashboards are separate kustomize roots so Flux can order them as
# two waves. Both must build, and nothing may sit directly under manifests/ —
# a file there would belong to neither wave.
for dir in "$manifest_dir"/folders "$manifest_dir"/dashboards; do
  [ -d "$dir" ] || { echo "✘ missing $dir" >&2; exit 1; }
  kustomize build "$dir" >/dev/null
done

if compgen -G "$manifest_dir/*.json" >/dev/null || [ -f "$manifest_dir/Kustomization" ]; then
  echo "✘ files found directly under $manifest_dir; they belong to no wave" >&2
  exit 1
fi

echo "✔ generated JSON and Kustomization are valid"
