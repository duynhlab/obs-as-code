package common

import (
	"fmt"
	"strings"

	"github.com/grafana/grafana-foundation-sdk/go/cog"
	"github.com/grafana/grafana-foundation-sdk/go/dashboardv2"

	"github.com/duynhlab/obs-as-code/internal/profile"
)

// A Prometheus variable query is NOT PromQL, and it does not go in `expr`.
//
// Measured on the cluster: with the query in `expr`, Grafana hands it to the
// datasource as PromQL and the datasource answers
//
//	422: unsupported function "label_values"
//
// so the variable's option list stays empty. The board still renders — panels
// use real PromQL, and `All` works because it substitutes allValue ".*" without
// needing options — but the dropdown offers nothing to pick, which is exactly
// what a reader sees: a Namespace picker stuck on All.
//
// The datasource's variable query is a small object of its own, and the
// label_values() string belongs in `query`. The shape here was taken from five
// boards that work on the same Grafana instance (Envoy Clusters, Envoy Gateway
// Global, CloudNativePG, PGDog), not from a guess: qryType 1 is "Label values",
// and refId is the constant their variable editor writes.
//
// Classic dashboards accept `query` as a bare string and parse label_values()
// themselves, which is why every V1 board here kept working. V2's
// QueryVariableSpec.Query is a DataQueryKind with no string variant, so the
// classic form cannot be expressed and reaching for `expr` looks like the
// natural substitute. It is not.
const (
	// promVariableLabelValues is qryType for the "Label values" variable type.
	promVariableLabelValues = 1
	// promVariableRefID is the refId Grafana's Prometheus variable editor writes.
	promVariableRefID = "PrometheusVariableQueryEditor-VariableQuery"
)

// VariableQuery builds the datasource variable query for a label_values()
// expression from internal/queries.
func VariableQuery(p profile.Profile, expr string) cog.Builder[dashboardv2.DataQueryKind] {
	return variableQuery{profile: p, expr: expr}
}

type variableQuery struct {
	profile profile.Profile
	expr    string
}

func (q variableQuery) Build() (dashboardv2.DataQueryKind, error) {
	if strings.TrimSpace(q.expr) == "" {
		return dashboardv2.DataQueryKind{}, fmt.Errorf("variable query: expression is empty")
	}

	datasource, err := q.profile.MetricsRef().Build()
	if err != nil {
		return dashboardv2.DataQueryKind{}, err
	}

	return dashboardv2.DataQueryKind{
		Kind:       "DataQuery",
		Group:      q.profile.MetricsPlugin,
		Version:    "v0",
		Datasource: &datasource,
		Spec: map[string]any{
			"qryType": promVariableLabelValues,
			"query":   q.expr,
			"refId":   promVariableRefID,
		},
	}, nil
}
