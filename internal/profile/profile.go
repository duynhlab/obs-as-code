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
	Name           string
	MetricsPlugin  string
	MetricsVar     string
	MetricsRegex   string
	MetricsDefault string
	LogsPlugin     string
	SourceURL      string
}

// Cluster returns the profile for the duynhlab homelab Grafana instance.
func Cluster() Profile {
	return Profile{
		Name:           "cluster",
		MetricsPlugin:  "prometheus",
		MetricsVar:     "ds",
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
	if p.MetricsDefault != "" {
		b = b.Current(dashboardv2.VariableOption{
			Text:  dashboardv2.StringOrArrayOfString{String: ref(p.MetricsDefault)},
			Value: dashboardv2.StringOrArrayOfString{String: ref(p.MetricsDefault)},
		})
	}
	return b
}

// ErrInvalid identifies an incomplete profile.
var ErrInvalid = errors.New("invalid profile")

// Validate reports every required field that is missing.
func (p Profile) Validate() error {
	missing := make([]string, 0, 3)
	for name, value := range map[string]string{
		"Name": p.Name, "MetricsPlugin": p.MetricsPlugin, "MetricsVar": p.MetricsVar,
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
