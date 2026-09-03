// Package profile describes the Grafana and datasource facts that dashboard
// code must not hardcode.
package profile

import (
	"errors"
	"fmt"
	"slices"

	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"
)

// Profile is the environment-specific input used to build a dashboard model.
// Kubernetes delivery settings deliberately live outside this package.
type Profile struct {
	Name          string
	MetricsPlugin string
	MetricsVar    string
	MetricsRegex  string
	// MetricsUID is the datasource's UID and MetricsDefault is its display
	// label. Grafana resolves a datasource reference by UID, so these two are
	// different kinds of string and must never be interchanged — see
	// MetricsVariable.
	MetricsUID     string
	MetricsDefault string
	LogsPlugin     string
	SourceURL      string
}

// Cluster returns the profile for the duynhlab homelab Grafana instance.
//
// MetricsUID names a datasource that must exist in that Grafana. Verify with:
//
//	kubectl -n monitoring exec deploy/grafana-deployment -- \
//	  wget -qO- http://localhost:3000/api/datasources/uid/victoriametrics-prometheus
//
// It is the VictoriaMetrics backend exposed under the portable `prometheus`
// plugin type, which is why MetricsPlugin is not the native VictoriaMetrics
// plugin.
func Cluster() Profile {
	return Profile{
		Name:           "cluster",
		MetricsPlugin:  "prometheus",
		MetricsVar:     "ds",
		MetricsUID:     "victoriametrics-prometheus",
		MetricsDefault: "VictoriaMetrics (Prometheus)",
		LogsPlugin:     "victoriametrics-logs-datasource",
		SourceURL:      "https://github.com/duynhlab/obs-as-code",
	}
}

// MetricsRef returns the V2 datasource reference used by Prometheus queries.
func (p Profile) MetricsRef() *dashboardv2.Dashboardv2DataQueryKindDatasourceBuilder {
	return dashboardv2.NewDashboardv2DataQueryKindDatasourceBuilder().Name(p.VarRef(p.MetricsVar))
}

// VarRef renders a dashboard variable reference such as ${ds}.
func (Profile) VarRef(name string) string { return "${" + name + "}" }

// MetricsVariable builds the datasource variable referenced by MetricsRef.
func (p Profile) MetricsVariable() *dashboardv2.DatasourceVariableBuilder {
	b := dashboardv2.NewDatasourceVariableBuilder(p.MetricsVar).
		Label("Datasource").
		PluginId(p.MetricsPlugin).
		AllowCustomValue(false)
	if p.MetricsRegex != "" {
		b = b.Regex(p.MetricsRegex)
	}
	// Value is the datasource UID; Text is only the label shown in the picker.
	// Setting both to the display name is what made every board render empty:
	// ${ds} expanded to "VictoriaMetrics (Prometheus)", Grafana looked that up
	// as a uid, found nothing, and issued no query. Measured 2026-09-03 —
	// /api/datasources/uid/victoriametrics-prometheus returns 200 while the
	// display name returns 404, and /api/ds/query behaves the same way.
	//
	// Current is set rather than left empty deliberately. This Grafana's default
	// datasource is the native VictoriaMetrics plugin, not a `prometheus`-typed
	// one, so an empty Current would let Grafana pick whichever prometheus
	// datasource it saw first — possibly the real Prometheus instead of
	// VictoriaMetrics.
	if p.MetricsUID != "" {
		text := p.MetricsDefault
		if text == "" {
			text = p.MetricsUID
		}
		b = b.Current(dashboardv2.VariableOption{
			Text:  dashboardv2.StringOrArrayOfString{String: ref(text)},
			Value: dashboardv2.StringOrArrayOfString{String: ref(p.MetricsUID)},
		})
	}
	return b
}

// ErrInvalid identifies an incomplete profile.
var ErrInvalid = errors.New("invalid profile")

// Validate reports every required field that is missing.
func (p Profile) Validate() error {
	// MetricsUID is required, not optional: without it MetricsVariable emits no
	// Current at all and Grafana picks a datasource for us. A board that queries
	// a datasource nobody chose is the failure this field exists to prevent.
	missing := make([]string, 0, 4)
	for name, value := range map[string]string{
		"Name": p.Name, "MetricsPlugin": p.MetricsPlugin, "MetricsVar": p.MetricsVar,
		"MetricsUID": p.MetricsUID,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	slices.Sort(missing)
	return fmt.Errorf("%w %q: empty %v", ErrInvalid, p.Name, missing)
}

func ref[T any](v T) *T { return &v }
