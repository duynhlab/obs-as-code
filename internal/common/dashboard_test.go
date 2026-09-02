package common_test

import (
	"testing"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

func meta(uid, title string) registry.Meta {
	return registry.Meta{UID: uid, Title: title, Owner: "platform", Publish: true}
}

func TestNewDashboardAppliesHouseDefaults(t *testing.T) {
	t.Parallel()

	p := profile.Cluster()

	board, err := common.NewDashboard(p, meta("defaults", "Defaults")).Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got, want := board.TimeSettings.AutoRefresh, "1m"; got != want {
		t.Errorf("AutoRefresh = %q, want %q", got, want)
	}
	if got, want := board.TimeSettings.From, "now-6h"; got != want {
		t.Errorf("TimeSettings.From = %q, want %q", got, want)
	}

	// Every generated board must be findable as generated.
	var tagged bool
	for _, tag := range board.Tags {
		if tag == common.Tag {
			tagged = true
		}
	}
	if !tagged {
		t.Errorf("Tags = %v, want it to include %q", board.Tags, common.Tag)
	}

	// Without the datasource variable, "${ds}" resolves to nothing and every
	// panel on the board renders empty.
	if len(board.Variables) == 0 {
		t.Fatal("board has no template variables; the datasource variable is missing")
	}
	variable := board.Variables[0].DatasourceVariableKind
	if variable == nil {
		t.Fatal("first variable is not a DatasourceVariable")
	}
	if got, want := variable.Spec.Name, p.MetricsVar; got != want {
		t.Errorf("first variable = %q, want %q", got, want)
	}

	// The link back to source is what stops someone editing a generated board
	// in the UI and losing the change on the next reconcile.
	if len(board.Links) == 0 {
		t.Fatal("board has no links; nothing points a reader at the source")
	}
	if got := board.Links[0].Url; got == nil || *got != p.SourceURL {
		t.Errorf("link url = %v, want %q", got, p.SourceURL)
	}
}

func TestNewDashboardCarriesExtraTags(t *testing.T) {
	t.Parallel()

	board, err := common.NewDashboard(profile.Cluster(), meta("tagged", "Tagged"), "http", "golden-signals").Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := []string{common.Tag, "owner:platform", "http", "golden-signals"}
	if len(board.Tags) != len(want) {
		t.Fatalf("Tags = %v, want %v", board.Tags, want)
	}
	for i, tag := range want {
		if board.Tags[i] != tag {
			t.Errorf("Tags[%d] = %q, want %q", i, board.Tags[i], tag)
		}
	}
}
