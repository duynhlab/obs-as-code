package render_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/profile"
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

	if !bytes.Equal(first, second) {
		t.Error("DashboardJSON() is not stable across calls")
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Error("DashboardJSON() output has no trailing newline; committed files should")
	}
}

func TestDashboard(t *testing.T) {
	t.Parallel()

	built, err := dashboard.NewDashboardBuilder("Example Board").Uid("example-board").Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	model, err := render.DashboardJSON(built)
	if err != nil {
		t.Fatalf("DashboardJSON() error = %v", err)
	}

	obj, err := render.Dashboard(profile.Cluster(), render.DashboardInput{
		UID:    "example-board",
		Folder: folders.Examples,
		Owner:  "platform",
		Model:  model,
	})
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}

	if got, want := obj.Path(), "grafanadashboard/example-board.yaml"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}

	got := decode(t, obj)

	if v, want := dig(t, got, "spec", "uid"), "example-board"; v != want {
		t.Errorf("spec.uid = %v, want %q", v, want)
	}
	// folderRef, not folder or folderUID — the CRD permits exactly one, and
	// this is the one that points at a resource this repo also generates.
	if v, want := dig(t, got, "spec", "folderRef"), folders.Examples.UID; v != want {
		t.Errorf("spec.folderRef = %v, want %q", v, want)
	}
	spec, ok := dig(t, got, "spec").(map[string]any)
	if !ok {
		t.Fatalf("spec is %T, want a map", dig(t, got, "spec"))
	}
	// The CRD treats the content sources and the folder fields as mutually
	// exclusive; spec.json and spec.folderRef are the two this repo sets.
	for _, forbidden := range []string{"folder", "folderUID", "gzipJson", "url", "configMapRef", "jsonnet", "grafanaCom", "oci"} {
		if _, present := spec[forbidden]; present {
			t.Errorf("spec.%s is set alongside spec.json and spec.folderRef, which the CRD forbids", forbidden)
		}
	}
	if v, want := dig(t, got, "metadata", "annotations", "obs-as-code/owner"), "platform"; v != want {
		t.Errorf("owner annotation = %v, want %q", v, want)
	}

	// The resource must carry the model verbatim. If it does not, Grafana
	// renders a different board than the JSON reviewed in the pull request.
	embedded, ok := dig(t, got, "spec", "json").(string)
	if !ok {
		t.Fatalf("spec.json is %T, want a string", dig(t, got, "spec", "json"))
	}
	if embedded != string(model) {
		t.Error("spec.json is not the model that was passed in")
	}
	// gzipJson was used first and abandoned: gzip output is not byte-stable
	// across Go releases, so a committed compressed blob broke `make diff`
	// whenever a toolchain differed.
	if _, present := spec["gzipJson"]; present {
		t.Error("spec.gzipJson is set; the model is stored uncompressed so the diff stays reviewable")
	}
}

func TestDashboardRejectsBadInput(t *testing.T) {
	t.Parallel()

	valid := []byte(`{"title":"x"}`)

	tests := []struct {
		name    string
		input   render.DashboardInput
		wantErr string
	}{
		{
			name:    "uid with underscore",
			input:   render.DashboardInput{UID: "bad_uid", Folder: folders.Examples, Model: valid},
			wantErr: "DNS-1123",
		},
		{
			name:    "empty uid",
			input:   render.DashboardInput{Folder: folders.Examples, Model: valid},
			wantErr: "uid is empty",
		},
		{
			name:    "empty model",
			input:   render.DashboardInput{UID: "ok", Folder: folders.Examples},
			wantErr: "model is empty",
		},
		{
			name:    "zero folder",
			input:   render.DashboardInput{UID: "ok", Model: valid},
			wantErr: "uid is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := render.Dashboard(profile.Cluster(), tt.input)
			if err == nil {
				t.Fatalf("Dashboard() = nil error, want one containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Dashboard() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestDashboardEnforcesSizeBudget(t *testing.T) {
	t.Parallel()

	// A board this big must fail the generate that produced it rather than an
	// apply hours later. The model is stored uncompressed, so its size is the
	// resource's size.
	huge := []byte(strings.Repeat(`{"panel":"x"},`, render.MaxObjectBytes/8))

	_, err := render.Dashboard(profile.Cluster(), render.DashboardInput{
		UID:    "too-big",
		Folder: folders.Examples,
		Model:  huge,
	})
	if err == nil {
		t.Fatal("Dashboard() = nil error, want the size budget to reject it")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Errorf("Dashboard() error = %v, want it to mention the budget", err)
	}
	if !strings.Contains(err.Error(), "spec.oci") {
		t.Errorf("Dashboard() error = %v, want it to point at the escape hatch", err)
	}
}
