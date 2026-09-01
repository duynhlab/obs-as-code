package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
	"github.com/grafana/grafana-foundation-sdk/go/timeseries"

	"github.com/duynhlab/obs-as-code/internal/render"
)

func TestDashboardJSONIsStable(t *testing.T) {
	t.Parallel()

	built, err := dashboard.NewDashboardBuilder("Stable").Uid("stable").Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	first, err := render.DashboardJSON(built)
	if err != nil {
		t.Fatalf("DashboardJSON() error = %v", err)
	}
	second, err := render.DashboardJSON(built)
	if err != nil {
		t.Fatalf("DashboardJSON() error = %v", err)
	}

	// The output is committed, so instability would surface as a phantom diff on
	// an unrelated pull request rather than as a failure here.
	if !bytes.Equal(first, second) {
		t.Error("DashboardJSON() is not stable across calls")
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Error("DashboardJSON() has no trailing newline; committed files should")
	}
}

func TestDashboardJSONIsImportable(t *testing.T) {
	t.Parallel()

	// The point of emitting plain JSON is that it can be imported anywhere, so
	// the top-level shape Grafana's importer reads must be present and correct.
	built, err := dashboard.NewDashboardBuilder("Importable").
		Uid("importable").
		Tags([]string{"obs-as-code"}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	out, err := render.DashboardJSON(built)
	if err != nil {
		t.Fatalf("DashboardJSON() error = %v", err)
	}

	for _, want := range []string{`"uid": "importable"`, `"title": "Importable"`, `"schemaVersion"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output is missing %s:\n%s", want, out)
		}
	}
}

func TestDashboardJSONEnforcesTheSizeBudget(t *testing.T) {
	t.Parallel()

	// Built from real panels rather than padding, so the failure means what the
	// message says: too much board, not too much text.
	b := dashboard.NewDashboardBuilder("Huge").Uid("huge")
	for range 4000 {
		b = b.WithPanel(timeseries.NewPanelBuilder().
			Title("A panel with a title long enough to take up room in the model").
			Description("And a description, because a board this size got there by accident.").
			Span(8).Height(8))
	}

	built, err := b.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	_, err = render.DashboardJSON(built)
	if err == nil {
		t.Fatal("DashboardJSON() = nil error, want the size budget to reject it")
	}
	if !strings.Contains(err.Error(), "split it") {
		t.Errorf("DashboardJSON() error = %v, want it to say what to do", err)
	}
}
