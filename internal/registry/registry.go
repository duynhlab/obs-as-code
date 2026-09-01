// Package registry is where every dashboard declares itself.
//
// A board file states what it is as data and supplies a Build function; it
// implements no methods. Registration happens from init, so a board is compiled
// in by being imported, and internal/catalog is the one place that imports them.
package registry

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/naming"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/render"
)

// Meta is what a board declares about itself.
//
// Deliberately not a folder: this repo emits dashboard JSON, and where a board
// is filed is a property of the Grafana it is installed into, not of the board.
// Whoever creates the GrafanaDashboard resource sets spec.folder.
type Meta struct {
	// UID is the Grafana UID, the output file's base name, and the path a
	// GrafanaDashboard's spec.oci points at. Changing it breaks that reference
	// loudly, which is the intended behaviour.
	UID string

	// Title is the display name in Grafana.
	Title string

	// Owner is the team or person to ask about this board. It is emitted as a
	// dashboard tag so it survives into Grafana and stays searchable.
	Owner string

	// Publish reports whether the board is written to the output tree. An
	// unpublished board is still registered and therefore still covered by every
	// conformance check.
	Publish bool
}

// OwnerTag renders Owner as a Grafana tag.
func (m Meta) OwnerTag() string { return "owner:" + m.Owner }

// Dashboard is a registered dashboard.
type Dashboard struct {
	Meta

	// Build returns the dashboard for a profile.
	//
	// It must not name a datasource UID or plugin type; take those from the
	// profile it is handed. It must set the same UID and title as Meta — Model
	// verifies that rather than trusting it.
	Build func(profile.Profile) *dashboard.DashboardBuilder
}

// Model returns the canonical dashboard JSON for a profile.
func (d Dashboard) Model(p profile.Profile) ([]byte, error) {
	if d.Build == nil {
		return nil, fmt.Errorf("dashboard %q: no Build function", d.UID)
	}

	built, err := d.Build(p).Build()
	if err != nil {
		return nil, fmt.Errorf("dashboard %q: build: %w", d.UID, err)
	}

	// A board whose model UID disagrees with its declared UID would be written to
	// one filename and served under another.
	if built.Uid == nil || *built.Uid != d.UID {
		return nil, fmt.Errorf("dashboard %q: model declares uid %q; they must match", d.UID, deref(built.Uid))
	}
	if built.Title == nil || *built.Title != d.Title {
		return nil, fmt.Errorf("dashboard %q: model declares title %q, registration declares %q", d.UID, deref(built.Title), d.Title)
	}
	if !slices.Contains(built.Tags, d.OwnerTag()) {
		return nil, fmt.Errorf("dashboard %q: model does not carry the owner tag %q; build it with common.NewDashboard, which adds it from Meta", d.UID, d.OwnerTag())
	}

	return render.DashboardJSON(built)
}

// Filename is where Model is written, relative to the output directory, and the
// path a GrafanaDashboard's spec.oci must name.
func (d Dashboard) Filename() string { return "dashboards/" + d.UID + ".json" }

// Registry holds registered dashboards. Use New for an isolated one; the
// package-level Add and All operate on a shared default, which is what board
// packages register into from init.
type Registry struct {
	mu        sync.RWMutex
	byUID     map[string]Dashboard
	titleUsed map[string]string
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{
		byUID:     make(map[string]Dashboard),
		titleUsed: make(map[string]string),
	}
}

var std = New()

// Add registers dashboards into the default Registry.
//
// It panics on a dashboard that cannot be registered. Registration runs from
// init, where there is no caller to return an error to, and a malformed board
// must stop the build rather than quietly vanish from the output.
func Add(dashboards ...Dashboard) {
	for _, d := range dashboards {
		if err := std.Add(d); err != nil {
			panic("registry: " + err.Error())
		}
	}
}

// All returns every registered dashboard, ordered by UID.
func All() []Dashboard { return std.All() }

// Published returns the dashboards the default Registry writes to disk.
func Published() []Dashboard { return std.Published() }

// Add registers one dashboard.
func (reg *Registry) Add(d Dashboard) error {
	if err := validateMeta(d.Meta); err != nil {
		return err
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	// Two boards sharing a UID means one silently overwrites the other, both on
	// disk and in Grafana.
	if existing, dup := reg.byUID[d.UID]; dup {
		return fmt.Errorf("uid %q already registered by %q", d.UID, existing.Title)
	}
	// A duplicate title is not fatal, but it produces two boards a human cannot
	// tell apart in a search — which is how redis.json and redis-exporter.json
	// ended up indistinguishable in the repo this replaces.
	if other, dup := reg.titleUsed[d.Title]; dup {
		return fmt.Errorf("title %q already used by uid %q", d.Title, other)
	}

	reg.byUID[d.UID] = d
	reg.titleUsed[d.Title] = d.UID

	return nil
}

// All returns every registered dashboard, ordered by UID so output and test
// subtests are stable across runs.
func (reg *Registry) All() []Dashboard {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	out := make([]Dashboard, 0, len(reg.byUID))
	for _, d := range reg.byUID {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b Dashboard) int { return strings.Compare(a.UID, b.UID) })

	return out
}

// Published returns the dashboards that will be written to disk.
func (reg *Registry) Published() []Dashboard {
	all := reg.All()
	out := make([]Dashboard, 0, len(all))
	for _, d := range all {
		if d.Publish {
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
		return fmt.Errorf("dashboard %q: owner is empty; someone has to be answerable for it", m.UID)
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
