// Package profile describes everything a dashboard must not assume about the
// Grafana instance and metrics backend it will be rendered for.
//
// Nothing outside this package may name a datasource UID or a datasource plugin
// type. The cluster and the local-stack both expose a datasource with UID
// "victoriametrics", but the cluster serves it through the VictoriaMetrics
// plugin while the local-stack serves it as a plain Prometheus datasource — a
// board that hardcoded either one would work in exactly one of the two places.
package profile

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// Profile is the set of environment facts a board is rendered against. It is
// passed by value: a Profile is configuration, never shared mutable state.
type Profile struct {
	// Name identifies the profile and names its output directory.
	Name string

	// MetricsPlugin is the Grafana datasource plugin type-id that backs metric
	// queries, e.g. "prometheus" or "victoriametrics-metrics-datasource".
	//
	// A Grafana data source variable holds exactly one plugin type — its
	// "Instance name filter" narrows instances, not types — so portability
	// across backends is a property of this field, not something a single board
	// can express. "prometheus" is the portable choice: the cluster exposes two
	// prometheus-type datasources over the same VictoriaMetrics backend, so a
	// board built this way also runs against a real Prometheus unchanged.
	MetricsPlugin string

	// MetricsVar is the name of the dashboard variable holding the metrics
	// datasource. Panels reference it as "${<MetricsVar>}".
	MetricsVar string

	// MetricsRegex optionally filters which datasource instances appear in the
	// variable's dropdown. Empty means no filter.
	MetricsRegex string

	// MetricsDefault is the datasource instance name preselected in the
	// dropdown. When absent from the target Grafana, Grafana falls back to the
	// first matching instance.
	MetricsDefault string

	// LogsPlugin is the plugin type-id backing log queries. Empty disables log
	// panels for this profile.
	LogsPlugin string

	// Namespace is where generated Kubernetes resources are created.
	Namespace string

	// InstanceLabels selects the target Grafana instances for the operator.
	InstanceLabels map[string]string

	// ResyncPeriod is how often the operator re-reconciles generated resources.
	ResyncPeriod time.Duration

	// SourceURL is linked from every board so a reader who opens it in the UI
	// can find the code that produced it.
	SourceURL string
}

// Cluster is the duynhlab homelab cluster: Grafana 13.2.0 managed by
// grafana-operator 5.24.0 in namespace "monitoring".
func Cluster() Profile {
	return Profile{
		Name:           "cluster",
		MetricsPlugin:  "prometheus",
		MetricsVar:     "ds",
		MetricsDefault: "VictoriaMetrics (Prometheus)",
		LogsPlugin:     "victoriametrics-logs-datasource",
		Namespace:      "monitoring",
		InstanceLabels: map[string]string{"dashboards": "grafana"},
		ResyncPeriod:   30 * time.Second,
		SourceURL:      "https://github.com/duynhlab/obs-as-code",
	}
}

// MetricsRef is the datasource reference every metric query and panel must use.
func (p Profile) MetricsRef() dashboard.DataSourceRef {
	return dashboard.DataSourceRef{
		Type: &p.MetricsPlugin,
		Uid:  ref(p.VarRef(p.MetricsVar)),
	}
}

// VarRef renders a dashboard variable reference, e.g. VarRef("ds") is "${ds}".
func (Profile) VarRef(name string) string {
	return "${" + name + "}"
}

// MetricsVariable builds the datasource variable that MetricsRef points at.
// Every board carries it; without it MetricsRef resolves to nothing.
func (p Profile) MetricsVariable() *dashboard.DatasourceVariableBuilder {
	b := dashboard.NewDatasourceVariableBuilder(p.MetricsVar).
		Label("Datasource").
		Type(p.MetricsPlugin).
		// A typed-in datasource name cannot resolve to an instance, so offering
		// the option only invites a board that renders nothing.
		AllowCustomValue(false)

	if p.MetricsRegex != "" {
		b = b.Regex(p.MetricsRegex)
	}
	if p.MetricsDefault != "" {
		b = b.Current(dashboard.VariableOption{
			Text:  dashboard.StringOrArrayOfString{String: ref(p.MetricsDefault)},
			Value: dashboard.StringOrArrayOfString{String: ref(p.MetricsDefault)},
		})
	}

	return b
}

// ErrInvalid reports a Profile that cannot produce valid resources.
var ErrInvalid = errors.New("invalid profile")

// Validate reports whether every field a renderer depends on is populated.
func (p Profile) Validate() error {
	missing := make([]string, 0, 5)
	for name, value := range map[string]string{
		"Name":          p.Name,
		"MetricsPlugin": p.MetricsPlugin,
		"MetricsVar":    p.MetricsVar,
		"Namespace":     p.Namespace,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(p.InstanceLabels) == 0 {
		missing = append(missing, "InstanceLabels")
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("%w %q: empty %v", ErrInvalid, p.Name, missing)
	}
	return nil
}

func ref[T any](v T) *T { return &v }
