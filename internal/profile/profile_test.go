package profile_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/profile"
)

func TestCluster(t *testing.T) {
	t.Parallel()

	p := profile.Cluster()

	if err := p.Validate(); err != nil {
		t.Fatalf("Cluster() does not validate: %v", err)
	}

	// Guarding the portable default deliberately. Pinning the cluster's own
	// VictoriaMetrics plugin type here would silently make every board
	// unusable against a plain Prometheus, which is a requirement, not a
	// preference.
	if got, want := p.MetricsPlugin, "prometheus"; got != want {
		t.Errorf("MetricsPlugin = %q, want %q (boards must stay portable to a plain Prometheus)", got, want)
	}
}

func TestProfileMetricsRef(t *testing.T) {
	t.Parallel()

	ref, err := profile.Cluster().MetricsRef().Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if ref.Name == nil || *ref.Name != "${ds}" {
		t.Errorf("Name = %v, want %q", ref.Name, "${ds}")
	}
}

func TestProfileVarRef(t *testing.T) {
	t.Parallel()

	if got, want := profile.Cluster().VarRef("job"), "${job}"; got != want {
		t.Errorf("VarRef(%q) = %q, want %q", "job", got, want)
	}
}

func TestProfileMetricsVariable(t *testing.T) {
	t.Parallel()

	v, err := profile.Cluster().MetricsVariable().Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got, want := v.Spec.Name, "ds"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	if got, want := v.Spec.PluginId, "prometheus"; got != want {
		t.Errorf("PluginId = %q, want %q", got, want)
	}
	if v.Spec.AllowCustomValue {
		t.Error("AllowCustomValue is true; a typed-in datasource name cannot resolve")
	}
}

func TestProfileValidate(t *testing.T) {
	t.Parallel()

	// Each case blanks exactly one required field on an otherwise valid
	// profile, so a failure names the field that stopped being checked.
	tests := []struct {
		name    string
		mutate  func(*profile.Profile)
		wantErr string
	}{
		{name: "valid", mutate: func(*profile.Profile) {}},
		{name: "no name", mutate: func(p *profile.Profile) { p.Name = "" }, wantErr: "Name"},
		{name: "no metrics plugin", mutate: func(p *profile.Profile) { p.MetricsPlugin = "" }, wantErr: "MetricsPlugin"},
		{name: "no metrics var", mutate: func(p *profile.Profile) { p.MetricsVar = "" }, wantErr: "MetricsVar"},
		{name: "no metrics uid", mutate: func(p *profile.Profile) { p.MetricsUID = "" }, wantErr: "MetricsUID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := profile.Cluster()
			tt.mutate(&p)

			err := p.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error naming %q", tt.wantErr)
			}
			if !errors.Is(err, profile.ErrInvalid) {
				t.Errorf("Validate() error does not wrap ErrInvalid: %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %v, want it to name %q", err, tt.wantErr)
			}
		})
	}
}

func TestProfileValidateReportsEveryMissingField(t *testing.T) {
	t.Parallel()

	// A profile missing several fields must report all of them at once, in a
	// stable order — reporting one per run turns fixing a fresh profile into a
	// guessing game, and an unstable order makes the message untestable.
	err := profile.Profile{}.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}

	const want = "[MetricsPlugin MetricsUID MetricsVar Name]"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Validate() = %v, want it to contain %s", err, want)
	}
}

// TestProfileMetricsVariableCurrentIsUID guards the bug that made every V2 board
// render empty: MetricsVariable set Current.Value to the datasource's DISPLAY
// NAME. Grafana resolves a datasource reference by UID, so ${ds} expanded to a
// string no lookup could match and every panel silently queried nothing.
//
// Measured on the homelab cluster 2026-09-03:
//
//	/api/datasources/uid/victoriametrics-prometheus      -> 200
//	/api/datasources/uid/VictoriaMetrics%20(Prometheus)  -> 404
//	/api/ds/query with uid=victoriametrics-prometheus    -> 76 frames
//	/api/ds/query with uid=VictoriaMetrics (Prometheus)  -> 404
//
// Nothing in this repo can prove the UID exists in Grafana — that is a cluster
// fact, not a code fact. What these assertions can prove is that the value has
// the SHAPE of a uid and is not merely a copy of the label, which is the whole
// failure. A green suite here still needs a human to look at a panel.
func TestProfileMetricsVariableCurrentIsUID(t *testing.T) {
	t.Parallel()

	v, err := profile.Cluster().MetricsVariable().Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	text, value := v.Spec.Current.Text.String, v.Spec.Current.Value.String
	if text == nil || value == nil {
		t.Fatalf("Current = %+v, want both Text and Value set", v.Spec.Current)
	}

	// The assertion that actually catches the bug. Text is a human label and
	// Value is a uid; they are different kinds of string, so they must never be
	// the same string.
	if *text == *value {
		t.Errorf("Current.Text and Current.Value are both %q; Value must be the datasource uid, not its display name", *value)
	}
	if got, want := *value, "victoriametrics-prometheus"; got != want {
		t.Errorf("Current.Value = %q, want %q", got, want)
	}
	if got, want := *text, "VictoriaMetrics (Prometheus)"; got != want {
		t.Errorf("Current.Text = %q, want %q", got, want)
	}

	// Shape check, so a future rename to any display-name-looking string fails
	// here rather than in a browser. Grafana uids are lowercase alphanumeric
	// plus dashes — no spaces, no parentheses, no capitals.
	if !uidShape.MatchString(*value) {
		t.Errorf("Current.Value = %q, want it to match %s", *value, uidShape)
	}
}

var uidShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
