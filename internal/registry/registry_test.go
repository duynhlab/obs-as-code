package registry_test

import (
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"

	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

func board(uid, title string, publish bool) registry.Dashboard {
	return registry.Dashboard{
		Meta: registry.Meta{UID: uid, Title: title, Owner: "platform", Publish: publish},
		Build: func(profile.Profile) cog.Builder[dashboardv2.Dashboard] {
			return dashboardv2.NewDashboardBuilder(title).Tags([]string{"owner:platform"})
		},
	}
}

func TestNewRejectsInvalidAndDuplicateEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []registry.Dashboard
		want    string
	}{
		{"valid", []registry.Dashboard{board("ok", "OK", true)}, ""},
		{"bad uid", []registry.Dashboard{board("Bad_UID", "Bad", true)}, "DNS-1123"},
		{"empty title", []registry.Dashboard{board("empty-title", " ", true)}, "title is empty"},
		{"empty owner", []registry.Dashboard{func() registry.Dashboard { d := board("no-owner", "No owner", true); d.Owner = ""; return d }()}, "owner is empty"},
		{"duplicate uid", []registry.Dashboard{board("same", "One", true), board("same", "Two", true)}, "already registered"},
		{"duplicate title", []registry.Dashboard{board("one", "Same", true), board("two", "Same", true)}, "already used"},
		{"empty delivery folder", []registry.Dashboard{func() registry.Dashboard {
			d := board("delivered", "Delivered", true)
			d.Delivery = &registry.Delivery{}
			return d
		}()}, "folder UID is empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := registry.New(test.entries...)
			if test.want == "" && err != nil {
				t.Fatalf("New() = %v", err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("New() = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRegistryViewsAreOrderedAndImmutable(t *testing.T) {
	t.Parallel()
	shipped := board("zulu", "Zulu", true)
	shipped.Delivery = &registry.Delivery{FolderUID: "platform"}
	reg := registry.MustNew(shipped, board("alpha", "Alpha", false), board("mike", "Mike", true))

	all := reg.All()
	if got := []string{all[0].UID, all[1].UID, all[2].UID}; strings.Join(got, ",") != "alpha,mike,zulu" {
		t.Fatalf("All order = %v", got)
	}
	all[0].UID = "mutated"
	if reg.All()[0].UID != "alpha" {
		t.Fatal("All returned registry storage instead of a copy")
	}
	if got := reg.Published(); len(got) != 2 {
		t.Fatalf("Published count = %d", len(got))
	}
	if got := reg.Deployable(); len(got) != 1 || got[0].UID != "zulu" {
		t.Fatalf("Deployable = %#v", got)
	}
}

func TestDashboardProducesV2ResourceAndCanonicalPaths(t *testing.T) {
	t.Parallel()
	dashboard := board("good-board", "Good Board", true)
	resource, err := dashboard.Resource(profile.Cluster())
	if err != nil {
		t.Fatal(err)
	}
	if resource.ApiVersion != "dashboard.grafana.app/v2" || resource.Kind != "Dashboard" || resource.Metadata.Name != "good-board" {
		t.Fatalf("resource identity = %#v", resource)
	}
	model, err := dashboard.Model(profile.Cluster())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(model), `"name": "good-board"`) {
		t.Errorf("model lacks metadata.name:\n%s", model)
	}
	if dashboard.Filename() != "dashboards/good-board.json" {
		t.Errorf("Filename = %q", dashboard.Filename())
	}
	if dashboard.ManifestFilename() != "manifests/dashboards/good-board.json" {
		t.Errorf("ManifestFilename = %q", dashboard.ManifestFilename())
	}
}

func TestDashboardBuildContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		dashboard registry.Dashboard
		want      string
	}{
		{"missing build", registry.Dashboard{Meta: registry.Meta{UID: "no-build", Title: "No Build", Owner: "platform"}}, "no Build"},
		{"title drift", func() registry.Dashboard {
			d := board("drift", "Declared", true)
			d.Build = func(profile.Profile) cog.Builder[dashboardv2.Dashboard] {
				return dashboardv2.NewDashboardBuilder("Actual").Tags([]string{"owner:platform"})
			}
			return d
		}(), "differs"},
		{"owner tag missing", func() registry.Dashboard {
			d := board("untagged", "Untagged", true)
			d.Build = func(profile.Profile) cog.Builder[dashboardv2.Dashboard] {
				return dashboardv2.NewDashboardBuilder("Untagged")
			}
			return d
		}(), "owner:platform"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := test.dashboard.Model(profile.Cluster())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Model() = %v, want %q", err, test.want)
			}
		})
	}
}
