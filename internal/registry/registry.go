// Package registry defines dashboard catalog entries and validates their identity.
package registry

import (
	"fmt"
	"slices"
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"
	"github.com/grafana/grafana-foundation-sdk/go/resource"

	"github.com/duynhlab/obs-as-code/internal/naming"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/render"
)

// Meta is the stable identity and ownership declared by a dashboard.
type Meta struct {
	UID     string
	Title   string
	Owner   string
	Publish bool
}

// OwnerTag returns the searchable ownership tag stored in Grafana.
func (m Meta) OwnerTag() string { return "owner:" + m.Owner }

// Delivery opts a dashboard into a deployable GrafanaManifest.
type Delivery struct {
	FolderUID string
}

// Dashboard is one catalog entry.
type Dashboard struct {
	Meta
	Build    func(profile.Profile) cog.Builder[dashboardv2.Dashboard]
	Delivery *Delivery
}

// Resource builds the raw dashboard.grafana.app/v2 resource.
func (d Dashboard) Resource(p profile.Profile) (resource.Manifest, error) {
	if d.Build == nil {
		return resource.Manifest{}, fmt.Errorf("dashboard %q: no Build function", d.UID)
	}
	spec, err := d.Build(p).Build()
	if err != nil {
		return resource.Manifest{}, fmt.Errorf("dashboard %q: build: %w", d.UID, err)
	}
	if spec.Title != d.Title {
		return resource.Manifest{}, fmt.Errorf("dashboard %q: model title %q differs from registration %q", d.UID, spec.Title, d.Title)
	}
	if !slices.Contains(spec.Tags, d.OwnerTag()) {
		return resource.Manifest{}, fmt.Errorf("dashboard %q: model lacks owner tag %q", d.UID, d.OwnerTag())
	}

	return resource.NewManifestBuilder().
		ApiVersion("dashboard.grafana.app/v2").
		Kind("Dashboard").
		Metadata(resource.Named(d.UID)).
		Spec(spec).
		Build()
}

// Model returns the canonical raw dashboard V2 JSON.
func (d Dashboard) Model(p profile.Profile) ([]byte, error) {
	manifest, err := d.Resource(p)
	if err != nil {
		return nil, err
	}
	return render.DashboardJSON(manifest)
}

// Filename is the raw dashboard path relative to a profile directory.
func (d Dashboard) Filename() string { return "dashboards/" + d.UID + ".json" }

// ManifestFilename is the deployable GrafanaManifest path.
func (d Dashboard) ManifestFilename() string { return "manifests/" + d.UID + ".json" }

// Registry is an immutable, deterministically ordered dashboard catalog.
type Registry struct{ dashboards []Dashboard }

// New validates and creates a registry.
func New(dashboards ...Dashboard) (*Registry, error) {
	byUID := make(map[string]string, len(dashboards))
	byTitle := make(map[string]string, len(dashboards))
	for _, d := range dashboards {
		if err := validateMeta(d.Meta); err != nil {
			return nil, err
		}
		if previous, ok := byUID[d.UID]; ok {
			return nil, fmt.Errorf("uid %q already registered by %q", d.UID, previous)
		}
		if previous, ok := byTitle[d.Title]; ok {
			return nil, fmt.Errorf("title %q already used by uid %q", d.Title, previous)
		}
		if d.Delivery != nil && strings.TrimSpace(d.Delivery.FolderUID) == "" {
			return nil, fmt.Errorf("dashboard %q: delivery folder UID is empty", d.UID)
		}
		byUID[d.UID], byTitle[d.Title] = d.Title, d.UID
	}
	copyOf := slices.Clone(dashboards)
	slices.SortFunc(copyOf, func(a, b Dashboard) int { return strings.Compare(a.UID, b.UID) })
	return &Registry{dashboards: copyOf}, nil
}

// MustNew creates a registry or panics during catalog initialization.
func MustNew(dashboards ...Dashboard) *Registry {
	registry, err := New(dashboards...)
	if err != nil {
		panic("registry: " + err.Error())
	}
	return registry
}

// All returns every dashboard in UID order.
func (r *Registry) All() []Dashboard { return slices.Clone(r.dashboards) }

// Published returns dashboards whose raw JSON is part of the artifact.
func (r *Registry) Published() []Dashboard {
	return filter(r.dashboards, func(d Dashboard) bool { return d.Publish })
}

// Deployable returns published dashboards opted into GrafanaManifest delivery.
func (r *Registry) Deployable() []Dashboard {
	return filter(r.dashboards, func(d Dashboard) bool { return d.Publish && d.Delivery != nil })
}

func filter(in []Dashboard, keep func(Dashboard) bool) []Dashboard {
	out := make([]Dashboard, 0, len(in))
	for _, d := range in {
		if keep(d) {
			out = append(out, d)
		}
	}
	return out
}

func validateMeta(m Meta) error {
	if err := naming.Validate("dashboard", m.UID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Title) == "" {
		return fmt.Errorf("dashboard %q: title is empty", m.UID)
	}
	if strings.TrimSpace(m.Owner) == "" {
		return fmt.Errorf("dashboard %q: owner is empty", m.UID)
	}
	return nil
}
