package registry_test

import (
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

// board is a minimal valid registration, with hooks for the negative cases.
func board(uid, title string, publish bool) registry.Dashboard {
	return registry.Dashboard{
		Meta: registry.Meta{
			UID:     uid,
			Title:   title,
			Owner:   "platform",
			Publish: publish,
		},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			return dashboard.NewDashboardBuilder(title).
				Uid(uid).
				Tags([]string{"owner:platform"})
		},
	}
}

func TestRegistryAddRejectsBadMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*registry.Dashboard)
		wantErr string
	}{
		{name: "valid", mutate: func(*registry.Dashboard) {}},
		{name: "bad uid", mutate: func(d *registry.Dashboard) { d.UID = "Bad_UID" }, wantErr: "DNS-1123"},
		{name: "empty uid", mutate: func(d *registry.Dashboard) { d.UID = "" }, wantErr: "uid is empty"},
		{name: "empty title", mutate: func(d *registry.Dashboard) { d.Title = "  " }, wantErr: "title is empty"},
		{name: "no owner", mutate: func(d *registry.Dashboard) { d.Owner = "" }, wantErr: "owner is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := board("ok-board", "OK Board", true)
			tt.mutate(&d)

			err := registry.New().Add(d)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Add() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Add() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Add() = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRegistryAddRejectsDuplicates(t *testing.T) {
	t.Parallel()

	t.Run("duplicate uid", func(t *testing.T) {
		t.Parallel()

		reg := registry.New()
		if err := reg.Add(board("dup", "First", true)); err != nil {
			t.Fatalf("first Add() = %v", err)
		}

		err := reg.Add(board("dup", "Second", true))
		if err == nil {
			t.Fatal("Add() = nil, want a duplicate-uid error")
		}
		if !strings.Contains(err.Error(), "already registered") {
			t.Errorf("Add() = %v, want it to report the duplicate", err)
		}
	})

	t.Run("duplicate title", func(t *testing.T) {
		t.Parallel()

		// Two boards with the same title are indistinguishable in Grafana's
		// search. The repo this replaces shipped exactly that.
		reg := registry.New()
		if err := reg.Add(board("first", "Same Title", true)); err != nil {
			t.Fatalf("first Add() = %v", err)
		}

		err := reg.Add(board("second", "Same Title", true))
		if err == nil {
			t.Fatal("Add() = nil, want a duplicate-title error")
		}
		if !strings.Contains(err.Error(), "already used") {
			t.Errorf("Add() = %v, want it to report the duplicate", err)
		}
	})
}

func TestRegistryAllIsOrderedByUID(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	for _, uid := range []string{"zulu", "alpha", "mike"} {
		if err := reg.Add(board(uid, strings.ToUpper(uid), true)); err != nil {
			t.Fatalf("Add(%q) = %v", uid, err)
		}
	}

	var got []string
	for _, r := range reg.All() {
		got = append(got, r.UID)
	}

	// Map iteration order is random, so without sorting the generated output
	// and the test subtest names would shuffle between runs.
	want := []string{"alpha", "mike", "zulu"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

func TestRegistryPublishedExcludesUnpublished(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	if err := reg.Add(board("shipped", "Shipped", true)); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(board("example", "Example", false)); err != nil {
		t.Fatal(err)
	}

	if got, want := len(reg.All()), 2; got != want {
		t.Errorf("All() has %d resources, want %d — an unpublished resource must still be registered and checked", got, want)
	}

	published := reg.Published()
	if got, want := len(published), 1; got != want {
		t.Fatalf("Published() has %d resources, want %d", got, want)
	}
	if got, want := published[0].UID, "shipped"; got != want {
		t.Errorf("Published()[0] = %q, want %q", got, want)
	}
}

func TestDashboardRenderRejectsUIDMismatch(t *testing.T) {
	t.Parallel()

	d := registry.Dashboard{
		Meta: registry.Meta{UID: "declared-uid", Title: "Mismatch", Owner: "platform", Publish: true},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			// Different UID than declared: the board would be filed under one
			// name and served under another.
			return dashboard.NewDashboardBuilder("Mismatch").Uid("actual-uid")
		},
	}

	_, err := d.Model(profile.Cluster())
	if err == nil {
		t.Fatal("Model() = nil error, want a uid mismatch")
	}
	if !strings.Contains(err.Error(), "they must match") {
		t.Errorf("Model() = %v, want it to report the mismatch", err)
	}
}

func TestDashboardRenderRejectsTitleMismatch(t *testing.T) {
	t.Parallel()

	d := registry.Dashboard{
		Meta: registry.Meta{UID: "title-drift", Title: "Declared Title", Owner: "platform", Publish: true},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			return dashboard.NewDashboardBuilder("Actual Title").Uid("title-drift")
		},
	}

	_, err := d.Model(profile.Cluster())
	if err == nil {
		t.Fatal("Model() = nil error, want a title mismatch")
	}
	if !strings.Contains(err.Error(), "registration declares") {
		t.Errorf("Model() = %v, want it to report the drift", err)
	}
}

func TestDashboardModelProducesImportableJSON(t *testing.T) {
	t.Parallel()

	d := board("good-board", "Good Board", true)

	model, err := d.Model(profile.Cluster())
	if err != nil {
		t.Fatalf("Model() error = %v", err)
	}
	if !strings.Contains(string(model), `"uid": "good-board"`) {
		t.Errorf("model does not carry its uid:\n%s", model)
	}

	// The filename is what a GrafanaDashboard's spec.oci path must name, so a
	// change here silently breaks every resource pointing at this board.
	if got, want := d.Filename(), "dashboards/good-board.json"; got != want {
		t.Errorf("Filename() = %q, want %q", got, want)
	}
}

func TestDashboardModelRequiresTheOwnerTag(t *testing.T) {
	t.Parallel()

	// Ownership used to live in a resource annotation. With plain JSON the tag is
	// the only place it survives, so a board that skips common.NewDashboard must
	// not slip through.
	d := registry.Dashboard{
		Meta: registry.Meta{UID: "untagged", Title: "Untagged", Owner: "platform", Publish: true},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			return dashboard.NewDashboardBuilder("Untagged").Uid("untagged")
		},
	}

	_, err := d.Model(profile.Cluster())
	if err == nil {
		t.Fatal("Model() = nil error, want the missing owner tag to be caught")
	}
	if !strings.Contains(err.Error(), "owner:platform") {
		t.Errorf("Model() = %v, want it to name the missing tag", err)
	}
}

func TestDashboardRenderWithoutBuildFails(t *testing.T) {
	t.Parallel()

	d := registry.Dashboard{Meta: registry.Meta{UID: "no-build", Title: "No Build", Owner: "platform"}}

	if _, err := d.Model(profile.Cluster()); err == nil {
		t.Fatal("Model() = nil error, want a missing-Build error")
	}
}
