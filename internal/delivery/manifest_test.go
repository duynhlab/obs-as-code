package delivery_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"

	"github.com/duynhlab/obs-as-code/internal/delivery"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

func deployableDashboard() registry.Dashboard {
	return registry.Dashboard{
		Meta:     registry.Meta{UID: "demo", Title: "Demo", Owner: "platform", Publish: true},
		Delivery: &registry.Delivery{FolderUID: "platform-infrastructure"},
		Build: func(profile.Profile) cog.Builder[dashboardv2.Dashboard] {
			return dashboardv2.NewDashboardBuilder("Demo").Tags([]string{"owner:platform"})
		},
	}
}

func TestDashboardWrapsV2Resource(t *testing.T) {
	t.Parallel()
	body, err := delivery.Dashboard(deployableDashboard(), profile.Cluster(), delivery.ClusterTarget())
	if err != nil {
		t.Fatal(err)
	}
	var manifest delivery.GrafanaManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.APIVersion != "grafana.integreatly.org/v1beta1" || manifest.Kind != "GrafanaManifest" {
		t.Fatalf("outer identity = %s %s", manifest.APIVersion, manifest.Kind)
	}
	if manifest.Metadata.Namespace != "monitoring" {
		t.Errorf("namespace = %q", manifest.Metadata.Namespace)
	}
	if manifest.Spec.Template.ApiVersion != "dashboard.grafana.app/v2" || manifest.Spec.Template.Kind != "Dashboard" {
		t.Fatalf("inner identity = %s %s", manifest.Spec.Template.ApiVersion, manifest.Spec.Template.Kind)
	}
	if got := manifest.Spec.Template.Metadata.Annotations["grafana.app/folder"]; got != "platform-infrastructure" {
		t.Errorf("folder annotation = %q", got)
	}
}

func TestDashboardRejectsMissingDelivery(t *testing.T) {
	t.Parallel()
	dashboard := deployableDashboard()
	dashboard.Delivery = nil
	if _, err := delivery.Dashboard(dashboard, profile.Cluster(), delivery.ClusterTarget()); err == nil {
		t.Fatal("Dashboard() accepted a dashboard without delivery metadata")
	}
}

func TestFolderAndKustomizationAreJSON(t *testing.T) {
	t.Parallel()
	folder, err := delivery.Folder("platform", "Platform", delivery.ClusterTarget())
	if err != nil {
		t.Fatal(err)
	}
	var manifest delivery.GrafanaManifest
	if err := json.Unmarshal(folder, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Spec.Template.ApiVersion != "folder.grafana.app/v1" || manifest.Spec.Template.Kind != "Folder" {
		t.Fatalf("folder identity = %s %s", manifest.Spec.Template.ApiVersion, manifest.Spec.Template.Kind)
	}
	body, err := delivery.Kustomization([]string{"platform.json", "demo.json"})
	if err != nil {
		t.Fatal(err)
	}
	var kustomization struct {
		Resources []string `json:"resources"`
	}
	if err := json.Unmarshal(body, &kustomization); err != nil {
		t.Fatal(err)
	}
	if len(kustomization.Resources) != 2 {
		t.Fatalf("resources = %v", kustomization.Resources)
	}
}
