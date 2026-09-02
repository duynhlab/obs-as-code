package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"
	"github.com/grafana/grafana-foundation-sdk/go/resource"

	"github.com/duynhlab/obs-as-code/internal/render"
)

func dashboardResource(t *testing.T, name, title string) resource.Manifest {
	t.Helper()
	spec, err := dashboardv2.NewDashboardBuilder(title).Tags([]string{"obs-as-code"}).Build()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := resource.NewManifestBuilder().ApiVersion("dashboard.grafana.app/v2").Kind("Dashboard").Metadata(resource.Named(name)).Spec(spec).Build()
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestDashboardJSONIsStableV2Resource(t *testing.T) {
	t.Parallel()
	manifest := dashboardResource(t, "stable", "Stable")
	first, err := render.DashboardJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := render.DashboardJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("DashboardJSON is unstable")
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Error("DashboardJSON has no trailing newline")
	}
	for _, want := range []string{`"apiVersion": "dashboard.grafana.app/v2"`, `"kind": "Dashboard"`, `"name": "stable"`, `"title": "Stable"`} {
		if !strings.Contains(string(first), want) {
			t.Errorf("output lacks %s:\n%s", want, first)
		}
	}
}

func TestDashboardJSONEnforcesSizeBudget(t *testing.T) {
	t.Parallel()
	manifest, err := resource.NewManifestBuilder().ApiVersion("dashboard.grafana.app/v2").Kind("Dashboard").Metadata(resource.Named("huge")).Spec(map[string]any{
		"title": "Huge", "description": strings.Repeat("x", render.MaxModelBytes),
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	_, err = render.DashboardJSON(manifest)
	if err == nil || !strings.Contains(err.Error(), "split it") {
		t.Fatalf("DashboardJSON() = %v, want size error", err)
	}
}

func TestJSONRendersAnyResourceCanonically(t *testing.T) {
	t.Parallel()
	out, err := render.JSON(map[string]any{"kind": "Kustomization", "apiVersion": "kustomize.config.k8s.io/v1beta1"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		t.Error("JSON has no trailing newline")
	}
}
