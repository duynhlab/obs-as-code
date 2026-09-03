package common_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/common"
	"github.com/duynhlab/obs-as-code/internal/panels"
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

// TestPanelIdentityGuards covers the four checks that stand between a mistyped
// board and a silently wrong one.
//
// The duplicate-key case is the one that matters most. V2 keeps panels in a map
// under spec.elements, so two panels sharing a key do not collide loudly — the
// second overwrites the first, and the board renders with one panel missing and
// no error anywhere. The layout still references both keys, so even the grid
// looks intact.
//
// The size guard is here for a related reason: a zero or over-wide panel is a
// layout Grafana accepts and renders unusably, rather than one it rejects.
func TestPanelIdentityGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*common.DashboardBuilder) *common.DashboardBuilder
		want  string
	}{
		{
			name: "panel before any row",
			build: func(b *common.DashboardBuilder) *common.DashboardBuilder {
				return b.Panel("orphan", panels.Timeseries(profile.Cluster(), "Orphan", "up", ""))
			},
			want: "has no row",
		},
		{
			name: "empty element key",
			build: func(b *common.DashboardBuilder) *common.DashboardBuilder {
				return b.Row("Signals").
					Panel("", panels.Timeseries(profile.Cluster(), "Nameless", "up", ""))
			},
			want: "key is empty",
		},
		{
			name: "duplicate element key",
			build: func(b *common.DashboardBuilder) *common.DashboardBuilder {
				return b.Row("Signals").
					Panel("dup", panels.Timeseries(profile.Cluster(), "First", "up", "")).
					Panel("dup", panels.Timeseries(profile.Cluster(), "Second", "up", ""))
			},
			want: "duplicated",
		},
		{
			name: "duplicate key across two rows",
			build: func(b *common.DashboardBuilder) *common.DashboardBuilder {
				return b.Row("First row").
					Panel("dup", panels.Timeseries(profile.Cluster(), "First", "up", "")).
					Row("Second row").
					Panel("dup", panels.Timeseries(profile.Cluster(), "Second", "up", ""))
			},
			want: "duplicated",
		},
		{
			name: "panel wider than the grid",
			build: func(b *common.DashboardBuilder) *common.DashboardBuilder {
				return b.Row("Signals").
					Panel("wide", panels.Timeseries(profile.Cluster(), "Wide", "up", "").Span(25).Height(8))
			},
			want: "invalid size",
		},
		{
			name: "panel with no height",
			build: func(b *common.DashboardBuilder) *common.DashboardBuilder {
				return b.Row("Signals").
					Panel("flat", panels.Timeseries(profile.Cluster(), "Flat", "up", "").Span(12).Height(0))
			},
			want: "invalid size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.build(common.NewDashboard(profile.Cluster(), meta("guards", "Guards"))).Build()
			if err == nil {
				t.Fatalf("Build() error = nil, want one mentioning %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Build() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestPanelKeysSurviveIntoElements is the positive half: distinct keys must all
// reach spec.elements, or the guard above would be passing for the wrong reason.
func TestPanelKeysSurviveIntoElements(t *testing.T) {
	t.Parallel()

	p := profile.Cluster()
	board, err := common.NewDashboard(p, meta("keys", "Keys")).
		Row("First row").
		Panel("a", panels.Timeseries(p, "A", "up", "").Span(12).Height(8)).
		Row("Second row").
		Panel("b", panels.Timeseries(p, "B", "up", "").Span(12).Height(8)).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	for _, key := range []string{"a", "b"} {
		if _, ok := board.Elements[key]; !ok {
			t.Errorf("element %q missing from spec.elements (got %v)", key, keysOf(board.Elements))
		}
	}
	if len(board.Elements) != 2 {
		t.Errorf("spec.elements has %d entries, want 2 (got %v)", len(board.Elements), keysOf(board.Elements))
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
