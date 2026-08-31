package render_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"math/rand/v2"
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
	for _, forbidden := range []string{"folder", "folderUID", "json", "url", "configMapRef"} {
		if _, present := spec[forbidden]; present {
			t.Errorf("spec.%s is set; the CRD treats the content and folder fields as mutually exclusive", forbidden)
		}
	}
	if v, want := dig(t, got, "metadata", "annotations", "obs-as-code/owner"), "platform"; v != want {
		t.Errorf("owner annotation = %v, want %q", v, want)
	}

	// The model must survive base64 → gunzip byte for byte. If it does not,
	// Grafana renders a different board than the JSON reviewed in the PR.
	encoded, ok := dig(t, got, "spec", "gzipJson").(string)
	if !ok {
		t.Fatalf("spec.gzipJson is %T, want a base64 string", dig(t, got, "spec", "gzipJson"))
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(decompressed, model) {
		t.Error("spec.gzipJson does not decode back to the model passed in")
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

	// Genuinely incompressible payload, so gzip cannot hide the size. A fixed
	// seed keeps the test reproducible. A board this big must fail the generate
	// that produced it rather than an apply hours later.
	huge := make([]byte, 4*render.MaxObjectBytes)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range huge {
		huge[i] = byte(rng.UintN(256))
	}

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
