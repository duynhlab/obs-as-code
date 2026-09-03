#!/usr/bin/env bash
set -euo pipefail

dashboard_dir=${1:-generated/cluster/dashboards}
container=obs-as-code-preview
image=${GRAFANA_IMAGE:-grafana/grafana:13.2.0}

cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
cleanup

# The datasource provisioning has to be mounted, or this preview cannot show a
# single number: every board pins its datasource variable to a uid, and a
# container with no datasources resolves none of them. Without this mount the
# provisioning file shipped beside this script was dead weight — which is how a
# datasource-resolution bug reached the cluster while `make preview` looked fine.
docker run --rm -d --name "$container" -p 3000:3000 \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  -e GF_AUTH_DISABLE_LOGIN_FORM=true \
  -v "$(pwd)/hack/provisioning/datasources:/etc/grafana/provisioning/datasources:ro" \
  "$image" >/dev/null

until curl --fail --silent http://localhost:3000/api/health >/dev/null; do sleep 1; done
for file in "$dashboard_dir"/*.json; do
  curl --fail --silent --show-error \
    -H 'Content-Type: application/json' \
    --data-binary "@$file" \
    http://localhost:3000/apis/dashboard.grafana.app/v2/namespaces/default/dashboards >/dev/null
done

echo "→ Grafana on http://localhost:3000 (anonymous admin). Ctrl-C to stop."
docker logs -f "$container"
