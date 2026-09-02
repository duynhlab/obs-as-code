// Package delivery wraps Grafana API resources in Kubernetes GrafanaManifest
// resources for Flux delivery.
package delivery

import (
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/folderv1"
	"github.com/grafana/grafana-foundation-sdk/go/resource"

	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
	"github.com/duynhlab/obs-as-code/internal/render"
)

// Target contains Kubernetes delivery facts, separate from dashboard profiles.
type Target struct {
	Namespace      string
	InstanceLabels map[string]string
	ResyncPeriod   string
}

// ClusterTarget returns the homelab Grafana Operator target.
func ClusterTarget() Target {
	return Target{
		Namespace:      "monitoring",
		InstanceLabels: map[string]string{"dashboards": "grafana"},
		ResyncPeriod:   "10m",
	}
}

// GrafanaManifest is the Kubernetes wrapper understood by Grafana Operator.
type GrafanaManifest struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   KubernetesMetadata  `json:"metadata"`
	Spec       GrafanaManifestSpec `json:"spec"`
}

// KubernetesMetadata is the metadata subset generated for delivery resources.
type KubernetesMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// GrafanaManifestSpec selects Grafana instances and carries the App Platform resource.
type GrafanaManifestSpec struct {
	InstanceSelector LabelSelector     `json:"instanceSelector"`
	ResyncPeriod     string            `json:"resyncPeriod"`
	Template         resource.Manifest `json:"template"`
}

// LabelSelector is the matchLabels subset used by this project.
type LabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

// Dashboard wraps one deployable dashboard for Grafana Operator.
func Dashboard(d registry.Dashboard, p profile.Profile, target Target) ([]byte, error) {
	if d.Delivery == nil {
		return nil, fmt.Errorf("dashboard %q is not deployable", d.UID)
	}
	inner, err := d.Resource(p)
	if err != nil {
		return nil, err
	}
	namespace := "default"
	inner.Metadata.Namespace = &namespace
	inner.Metadata.Annotations = map[string]string{"grafana.app/folder": d.Delivery.FolderUID}
	return wrap(d.UID, inner, target)
}

// Folder creates a deployable Grafana folder resource.
func Folder(uid, title string, target Target) ([]byte, error) {
	inner, err := folderv1.Manifest(uid, folderv1.NewFolderBuilder(title)).Build()
	if err != nil {
		return nil, fmt.Errorf("build folder %q: %w", uid, err)
	}
	namespace := "default"
	inner.Metadata.Namespace = &namespace
	return wrap(uid+"-folder", inner, target)
}

func wrap(name string, inner resource.Manifest, target Target) ([]byte, error) {
	if target.Namespace == "" || len(target.InstanceLabels) == 0 || target.ResyncPeriod == "" {
		return nil, fmt.Errorf("delivery target is incomplete")
	}
	manifest := GrafanaManifest{
		APIVersion: "grafana.integreatly.org/v1beta1",
		Kind:       "GrafanaManifest",
		Metadata: KubernetesMetadata{
			Name: name, Namespace: target.Namespace,
			Labels: map[string]string{"app.kubernetes.io/managed-by": "obs-as-code"},
		},
		Spec: GrafanaManifestSpec{
			InstanceSelector: LabelSelector{MatchLabels: target.InstanceLabels},
			ResyncPeriod:     target.ResyncPeriod,
			Template:         inner,
		},
	}
	return render.JSON(manifest)
}

// Kustomization renders the artifact-local resource list. Kustomize recognizes
// the extensionless filename "Kustomization" while its contents remain JSON.
func Kustomization(resources []string) ([]byte, error) {
	return render.JSON(struct {
		APIVersion string   `json:"apiVersion"`
		Kind       string   `json:"kind"`
		Resources  []string `json:"resources"`
	}{APIVersion: "kustomize.config.k8s.io/v1beta1", Kind: "Kustomization", Resources: resources})
}
