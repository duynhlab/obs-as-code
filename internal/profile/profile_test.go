package profile_test

import (
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

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
	if got, want := p.Namespace, "monitoring"; got != want {
		t.Errorf("Namespace = %q, want %q (grafana-operator runs watchNamespaces=monitoring)", got, want)
	}
	if got, want := p.InstanceLabels["dashboards"], "grafana"; got != want {
		t.Errorf(`InstanceLabels["dashboards"] = %q, want %q`, got, want)
	}
}

func TestProfileMetricsRef(t *testing.T) {
	t.Parallel()

	ref := profile.Cluster().MetricsRef()

	if ref.Type == nil || *ref.Type != "prometheus" {
		t.Errorf("Type = %v, want %q", ref.Type, "prometheus")
	}
	// The UID must be the variable reference, never a literal instance UID.
	if ref.Uid == nil || *ref.Uid != "${ds}" {
		t.Errorf("Uid = %v, want %q", ref.Uid, "${ds}")
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

	if got, want := v.Name, "ds"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	// For a datasource variable Grafana stores the plugin type-id in `query`.
	if v.Query == nil {
		t.Fatal("Query is nil, want the plugin type-id")
	}
	if got, want := v.Query.String, "prometheus"; got == nil || *got != want {
		t.Errorf("Query = %v, want %q", got, want)
	}
	if v.AllowCustomValue == nil || *v.AllowCustomValue {
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
		{name: "no namespace", mutate: func(p *profile.Profile) { p.Namespace = "" }, wantErr: "Namespace"},
		{name: "no instance labels", mutate: func(p *profile.Profile) { p.InstanceLabels = nil }, wantErr: "InstanceLabels"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := profile.Cluster()
			p.InstanceLabels = maps.Clone(p.InstanceLabels)
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
	err := profile.Profile{ResyncPeriod: time.Second}.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}

	const want = "[InstanceLabels MetricsPlugin MetricsVar Name Namespace]"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("Validate() = %v, want it to contain %s", err, want)
	}
}
