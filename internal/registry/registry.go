// Package registry is where every dashboard and alert group declares itself.
//
// A resource file states what it is as data and supplies a Build function; it
// implements no methods. The Resource interface below is satisfied once, here,
// by Dashboard and AlertGroup — so the generator and the conformance suite can
// treat every resource uniformly without each of fifty resource files paying
// for it. This is the shape http.HandlerFunc uses, for the same reason.
//
// Registration happens from init, so a resource package is compiled in by being
// imported. internal/catalog is the one place that does that importing.
package registry

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/grafana/grafana-foundation-sdk/go/alerting"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/naming"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/render"
)

// Meta is what every resource declares about itself, regardless of kind.
type Meta struct {
	// UID is the Grafana UID and the Kubernetes resource name. It must equal
	// the Go file's base name so a reader can find one from the other.
	UID string

	// Title is the display name in Grafana.
	Title string

	// Folder is where the resource is filed. Reference a folders package
	// variable — a string literal cannot be checked or renamed safely.
	Folder folders.Folder

	// Owner is the team or person to ask about this resource. It becomes an
	// annotation on the generated object, because at fifty boards "whose is
	// this" is the expensive question.
	Owner string

	// Publish reports whether the resource is written to the output tree.
	//
	// A resource with Publish false is still registered, and therefore still
	// covered by every conformance check. That is how the example board keeps
	// the harness exercised without shipping anything to the cluster.
	Publish bool
}

// Resource is anything that renders into Grafana Operator objects.
//
// Only Dashboard and AlertGroup implement it, and they do so in this file.
// Resource files never implement it.
type Resource interface {
	// Describe returns the resource's metadata.
	Describe() Meta

	// Render produces the objects for one profile.
	Render(profile.Profile) ([]render.Object, error)
}

// Dashboard is a registered dashboard.
type Dashboard struct {
	Meta

	// Build returns the dashboard for a profile.
	//
	// It must not name a datasource UID or plugin type; take those from the
	// profile it is handed. It must set the same UID as Meta.UID — Render
	// verifies that rather than trusting it.
	Build func(profile.Profile) *dashboard.DashboardBuilder
}

// Describe implements Resource.
func (d Dashboard) Describe() Meta { return d.Meta }

// Model returns the canonical dashboard JSON for a profile.
//
// Both Render and cmd/generate go through here, so the JSON committed for review
// and the JSON compressed into the resource are the same bytes by construction
// rather than by coincidence.
func (d Dashboard) Model(p profile.Profile) ([]byte, error) {
	if d.Build == nil {
		return nil, fmt.Errorf("dashboard %q: no Build function", d.UID)
	}

	built, err := d.Build(p).Build()
	if err != nil {
		return nil, fmt.Errorf("dashboard %q: build: %w", d.UID, err)
	}

	// A board whose model UID disagrees with its declared UID would be filed
	// under one name and served under another; spec.uid then silently wins.
	if built.Uid == nil || *built.Uid != d.UID {
		return nil, fmt.Errorf("dashboard %q: model declares uid %q; they must match", d.UID, deref(built.Uid))
	}
	if built.Title == nil || *built.Title != d.Title {
		return nil, fmt.Errorf("dashboard %q: model declares title %q, registration declares %q", d.UID, deref(built.Title), d.Title)
	}

	return render.DashboardJSON(built)
}

// Render implements Resource.
func (d Dashboard) Render(p profile.Profile) ([]render.Object, error) {
	model, err := d.Model(p)
	if err != nil {
		return nil, err
	}

	obj, err := render.Dashboard(p, render.DashboardInput{
		UID:    d.UID,
		Folder: d.Folder,
		Owner:  d.Owner,
		Model:  model,
	})
	if err != nil {
		return nil, err
	}

	return []render.Object{obj}, nil
}

// AlertGroup is a registered alert rule group.
type AlertGroup struct {
	Meta

	// Build returns the rule group for a profile.
	Build func(profile.Profile) *alerting.RuleGroupBuilder
}

// Describe implements Resource.
func (g AlertGroup) Describe() Meta { return g.Meta }

// Render implements Resource.
func (g AlertGroup) Render(p profile.Profile) ([]render.Object, error) {
	if g.Build == nil {
		return nil, fmt.Errorf("alert group %q: no Build function", g.UID)
	}

	built, err := g.Build(p).Build()
	if err != nil {
		return nil, fmt.Errorf("alert group %q: build: %w", g.UID, err)
	}

	obj, err := render.AlertRuleGroup(p, render.AlertRuleGroupInput{
		UID:    g.UID,
		Folder: g.Folder,
		Owner:  g.Owner,
		Group:  built,
	})
	if err != nil {
		return nil, err
	}

	return []render.Object{obj}, nil
}

// Registry holds registered resources. Use New for an isolated one; the
// package-level Add and All operate on a shared default, which is what resource
// packages register into from init.
type Registry struct {
	mu        sync.RWMutex
	byUID     map[string]Resource
	titleUsed map[string]string
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		byUID:     make(map[string]Resource),
		titleUsed: make(map[string]string),
	}
}

var std = New()

// Add registers resources into the default Registry.
//
// It panics on a resource that cannot be registered. Registration runs from
// init, where there is no caller to return an error to, and a malformed
// resource must stop the build rather than quietly vanish from the output.
func Add(resources ...Resource) {
	for _, r := range resources {
		if err := std.Add(r); err != nil {
			panic("registry: " + err.Error())
		}
	}
}

// All returns every resource registered in the default Registry, ordered by UID.
func All() []Resource { return std.All() }

// Published returns the resources the default Registry will write to disk.
func Published() []Resource { return std.Published() }

// Folders returns the distinct folders referenced by published resources in the
// default Registry, in folders declaration order.
//
// Only referenced folders are returned: creating a folder nothing files into
// leaves an empty folder in Grafana that nobody can explain.
func Folders() []folders.Folder { return std.Folders() }

// Add registers one resource.
func (reg *Registry) Add(r Resource) error {
	if r == nil {
		return fmt.Errorf("nil resource")
	}

	m := r.Describe()
	if err := validateMeta(m); err != nil {
		return err
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	// Two resources sharing a UID means one silently overwrites the other in
	// Grafana, and the loser is whichever the operator syncs second.
	if existing, dup := reg.byUID[m.UID]; dup {
		return fmt.Errorf("uid %q already registered by %q", m.UID, existing.Describe().Title)
	}
	// A duplicate title is not fatal, but it produces two boards a human cannot
	// tell apart in a search — which is how redis.json and redis-exporter.json
	// ended up indistinguishable in the repo this replaces.
	if other, dup := reg.titleUsed[m.Title]; dup {
		return fmt.Errorf("title %q already used by uid %q", m.Title, other)
	}

	reg.byUID[m.UID] = r
	reg.titleUsed[m.Title] = m.UID

	return nil
}

// All returns every registered resource, ordered by UID so output and test
// subtests are stable across runs.
func (reg *Registry) All() []Resource {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	out := make([]Resource, 0, len(reg.byUID))
	for _, r := range reg.byUID {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Resource) int {
		return strings.Compare(a.Describe().UID, b.Describe().UID)
	})

	return out
}

// Published returns the resources that will be written to disk.
func (reg *Registry) Published() []Resource {
	all := reg.All()
	out := make([]Resource, 0, len(all))
	for _, r := range all {
		if r.Describe().Publish {
			out = append(out, r)
		}
	}
	return out
}

// Folders returns the distinct folders referenced by published resources.
func (reg *Registry) Folders() []folders.Folder {
	referenced := make(map[string]bool)
	for _, r := range reg.Published() {
		referenced[r.Describe().Folder.UID] = true
	}

	out := make([]folders.Folder, 0, len(referenced))
	for _, f := range folders.All() {
		if referenced[f.UID] {
			out = append(out, f)
		}
	}
	return out
}

func validateMeta(m Meta) error {
	if err := naming.Validate("resource", m.UID); err != nil {
		return err
	}
	if strings.TrimSpace(m.Title) == "" {
		return fmt.Errorf("resource %q: title is empty", m.UID)
	}
	if err := m.Folder.Validate(); err != nil {
		return fmt.Errorf("resource %q: %w", m.UID, err)
	}
	if strings.TrimSpace(m.Owner) == "" {
		return fmt.Errorf("resource %q: owner is empty; someone has to be answerable for it", m.UID)
	}
	return nil
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
