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
//
// The folder is declared here, on the board, and nowhere else. It used to exist
// in two places: boards named a FolderUID while the generator wrote the folder
// resource from a literal of its own, with nothing making the two agree. A
// board naming a folder that was never created fails at apply time with
// "folders.folder.grafana.app ... not found" — on the cluster, where CI cannot
// see it.
type Delivery struct {
	FolderUID string
	// Title is the folder's display name in Grafana. Boards sharing a
	// FolderUID must agree on it, which New enforces.
	Title string
}

// Folder is one Grafana folder the artifact must create.
type Folder struct {
	UID   string
	Title string
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
//
// Dashboards and folders live in sibling directories so Flux can apply them as
// two waves with the dashboards depending on the folders. The Grafana operator
// applies each GrafanaManifest independently with no ordering guarantee, and a
// dashboard naming a folder that does not exist yet fails outright — ordering
// within one wave guarantees nothing, and two waves need two paths.
func (d Dashboard) ManifestFilename() string { return "manifests/dashboards/" + d.UID + ".json" }

// Registry is an immutable, deterministically ordered dashboard catalog.
type Registry struct{ dashboards []Dashboard }

// New validates and creates a registry.
func New(dashboards ...Dashboard) (*Registry, error) {
	byUID := make(map[string]string, len(dashboards))
	byTitle := make(map[string]string, len(dashboards))
	folderTitles := make(map[string]string)
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
		if err := validateDelivery(d, folderTitles); err != nil {
			return nil, err
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

// Folders returns the distinct folders declared by deployable dashboards, in
// UID order so the artifact does not churn on catalog order.
//
// The generator writes exactly these, which is what makes a board's declared
// folder and the folder that gets created the same fact.
func (r *Registry) Folders() []Folder {
	seen := make(map[string]bool)
	var out []Folder
	for _, d := range r.Deployable() {
		if seen[d.Delivery.FolderUID] {
			continue
		}
		seen[d.Delivery.FolderUID] = true
		out = append(out, Folder{UID: d.Delivery.FolderUID, Title: d.Delivery.Title})
	}
	slices.SortFunc(out, func(a, b Folder) int { return strings.Compare(a.UID, b.UID) })
	return out
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

// validateDelivery checks the folder a board declares, and that boards sharing
// a folder agree on its title — otherwise the rendered folder would depend on
// which board was registered last.
func validateDelivery(d Dashboard, folderTitles map[string]string) error {
	if d.Delivery == nil {
		return nil
	}
	// The folder UID becomes a Kubernetes resource name, so a value the CRD
	// would reject must fail here rather than at apply time.
	if err := naming.Validate("folder", d.Delivery.FolderUID); err != nil {
		return fmt.Errorf("dashboard %q: %w", d.UID, err)
	}
	title := strings.TrimSpace(d.Delivery.Title)
	if title == "" {
		return fmt.Errorf("dashboard %q: delivery folder title is empty", d.UID)
	}
	if previous, ok := folderTitles[d.Delivery.FolderUID]; ok && previous != title {
		return fmt.Errorf("folder %q has two titles, %q and %q; boards sharing a folder must agree", d.Delivery.FolderUID, previous, title)
	}
	folderTitles[d.Delivery.FolderUID] = title
	return nil
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
