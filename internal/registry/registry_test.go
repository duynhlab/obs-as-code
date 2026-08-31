package registry_test

import (
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

// board is a minimal valid registration, with hooks for the negative cases.
func board(uid, title string, publish bool) registry.Dashboard {
	return registry.Dashboard{
		Meta: registry.Meta{
			UID:     uid,
			Title:   title,
			Folder:  folders.Examples,
			Owner:   "platform",
			Publish: publish,
		},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			return dashboard.NewDashboardBuilder(title).Uid(uid)
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
		{name: "zero folder", mutate: func(d *registry.Dashboard) { d.Folder = folders.Folder{} }, wantErr: "uid is empty"},
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
		got = append(got, r.Describe().UID)
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
	if got, want := published[0].Describe().UID, "shipped"; got != want {
		t.Errorf("Published()[0] = %q, want %q", got, want)
	}
}

func TestRegistryFoldersOnlyReturnsReferencedOnes(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	if err := reg.Add(board("shipped", "Shipped", true)); err != nil {
		t.Fatal(err)
	}

	got := reg.Folders()
	if len(got) != 1 || got[0].UID != folders.Examples.UID {
		t.Fatalf("Folders() = %v, want only %q", got, folders.Examples.UID)
	}

	// A folder nothing files into leaves an unexplainable empty folder in
	// Grafana, so unreferenced declarations must not be emitted.
	if len(folders.All()) <= len(got) {
		t.Skip("every declared folder happens to be referenced; nothing to assert")
	}
}

func TestDashboardRenderRejectsUIDMismatch(t *testing.T) {
	t.Parallel()

	d := registry.Dashboard{
		Meta: registry.Meta{
			UID: "declared-uid", Title: "Mismatch",
			Folder: folders.Examples, Owner: "platform", Publish: true,
		},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			// Different UID than declared: the board would be filed under one
			// name and served under another.
			return dashboard.NewDashboardBuilder("Mismatch").Uid("actual-uid")
		},
	}

	_, err := d.Render(profile.Cluster())
	if err == nil {
		t.Fatal("Render() = nil error, want a uid mismatch")
	}
	if !strings.Contains(err.Error(), "they must match") {
		t.Errorf("Render() = %v, want it to report the mismatch", err)
	}
}

func TestDashboardRenderRejectsTitleMismatch(t *testing.T) {
	t.Parallel()

	d := registry.Dashboard{
		Meta: registry.Meta{
			UID: "title-drift", Title: "Declared Title",
			Folder: folders.Examples, Owner: "platform", Publish: true,
		},
		Build: func(profile.Profile) *dashboard.DashboardBuilder {
			return dashboard.NewDashboardBuilder("Actual Title").Uid("title-drift")
		},
	}

	_, err := d.Render(profile.Cluster())
	if err == nil {
		t.Fatal("Render() = nil error, want a title mismatch")
	}
	if !strings.Contains(err.Error(), "registration declares") {
		t.Errorf("Render() = %v, want it to report the drift", err)
	}
}

func TestDashboardRenderProducesOneObject(t *testing.T) {
	t.Parallel()

	objs, err := board("good-board", "Good Board", true).Render(profile.Cluster())
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("Render() returned %d objects, want 1", len(objs))
	}
	if got, want := objs[0].Kind, "GrafanaDashboard"; got != want {
		t.Errorf("Kind = %q, want %q", got, want)
	}
	if got, want := objs[0].Name, "good-board"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
}

func TestDashboardRenderWithoutBuildFails(t *testing.T) {
	t.Parallel()

	d := registry.Dashboard{Meta: registry.Meta{UID: "no-build", Title: "No Build", Folder: folders.Examples, Owner: "platform"}}

	if _, err := d.Render(profile.Cluster()); err == nil {
		t.Fatal("Render() = nil error, want a missing-Build error")
	}
}
