// Package check validates the rendered resources that are actually shipped.
package check

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/duynhlab/obs-as-code/internal/promql"
)

// Violation is one broken conformance rule.
type Violation struct {
	Resource string
	Rule     string
	Detail   string
}

func (v Violation) String() string { return fmt.Sprintf("%s: [%s] %s", v.Resource, v.Rule, v.Detail) }

const (
	RulePanelNoTarget   = "panel-no-target"
	RulePanelNoTitle    = "panel-no-title"
	RuleGridOverlap     = "grid-overlap"
	RuleDuplicateRefID  = "duplicate-refid"
	RuleDatasourceRef   = "datasource-ref"
	RuleNoDatasourceVar = "no-datasource-variable"
	RuleQuerySyntax     = "query-syntax"
	RuleRateInterval    = "rate-interval"
	RuleForbiddenLabel  = "forbidden-label"
	RuleDBNamespace     = "db-namespace"
)

var forbiddenLabels = []string{
	"user_id", "userid", "order_id", "orderid", "session_id", "sessionid",
	"payment_id", "paymentid", "promo_code", "promocode", "request_id", "requestid",
	"trace_id", "traceid", "email", "ip", "ip_address", "remote_addr",
}

// These small wire structs intentionally describe only the Dashboard V2 fields
// the rules need. Checks stay coupled to the artifact, not public-preview SDK APIs.
type dashboardResource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Spec       struct {
		Variables []variable         `json:"variables"`
		Elements  map[string]element `json:"elements"`
		Layout    layout             `json:"layout"`
	} `json:"spec"`
}

type variable struct {
	Kind string `json:"kind"`
	Spec struct {
		Name     string `json:"name"`
		PluginID string `json:"pluginId"`
	} `json:"spec"`
}

type element struct {
	Kind string `json:"kind"`
	Spec struct {
		Title string `json:"title"`
		Data  struct {
			Spec struct {
				Queries []panelQuery `json:"queries"`
			} `json:"spec"`
		} `json:"data"`
	} `json:"spec"`
}

type panelQuery struct {
	Spec struct {
		RefID string `json:"refId"`
		Query struct {
			Group      string `json:"group"`
			Datasource *struct {
				Name string `json:"name"`
			} `json:"datasource"`
			Spec struct {
				Expr string `json:"expr"`
			} `json:"spec"`
		} `json:"query"`
	} `json:"spec"`
}

type layout struct {
	Spec struct {
		Rows []struct {
			Spec struct {
				Layout layout `json:"layout"`
			} `json:"spec"`
		} `json:"rows"`
		Items []gridItem `json:"items"`
	} `json:"spec"`
}

type gridItem struct {
	Spec struct {
		X       int `json:"x"`
		Y       int `json:"y"`
		Width   int `json:"width"`
		Height  int `json:"height"`
		Element struct {
			Name string `json:"name"`
		} `json:"element"`
	} `json:"spec"`
}

// Dashboard runs every rule against a rendered Dashboard V2 resource.
func Dashboard(uid string, model []byte) []Violation {
	var board dashboardResource
	if err := json.Unmarshal(model, &board); err != nil {
		return []Violation{{Resource: uid, Rule: RuleQuerySyntax, Detail: "resource is not valid JSON: " + err.Error()}}
	}
	if board.APIVersion != "dashboard.grafana.app/v2" || board.Kind != "Dashboard" {
		return []Violation{{Resource: uid, Rule: RuleQuerySyntax, Detail: fmt.Sprintf("want dashboard.grafana.app/v2 Dashboard, got %q %q", board.APIVersion, board.Kind)}}
	}

	datasources := make(map[string]string)
	for _, variable := range board.Spec.Variables {
		if variable.Kind == "DatasourceVariable" && variable.Spec.Name != "" {
			datasources["${"+variable.Spec.Name+"}"] = variable.Spec.PluginID
		}
	}
	var out []Violation
	if len(datasources) == 0 {
		out = append(out, Violation{Resource: uid, Rule: RuleNoDatasourceVar, Detail: "board declares no DatasourceVariable; build it with common.NewDashboard"})
	}

	keys := make([]string, 0, len(board.Spec.Elements))
	for key := range board.Spec.Elements {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		element := board.Spec.Elements[key]
		if element.Kind == "Panel" {
			out = append(out, checkPanel(uid, key, element, datasources)...)
		}
	}
	return append(out, checkLayout(uid, board.Spec.Elements, board.Spec.Layout)...)
}

func checkPanel(uid, key string, panel element, datasources map[string]string) []Violation {
	label := panel.Spec.Title
	var out []Violation
	if label == "" {
		label = "(untitled " + key + ")"
		out = append(out, Violation{Resource: uid, Rule: RulePanelNoTitle, Detail: fmt.Sprintf("panel element %q has no title", key)})
	}
	queries := panel.Spec.Data.Spec.Queries
	if len(queries) == 0 {
		return append(out, Violation{Resource: uid, Rule: RulePanelNoTarget, Detail: fmt.Sprintf("panel %q has no query, so it renders empty", label)})
	}

	seen := make(map[string]bool, len(queries))
	for _, target := range queries {
		if target.Spec.RefID != "" && seen[target.Spec.RefID] {
			out = append(out, Violation{Resource: uid, Rule: RuleDuplicateRefID, Detail: fmt.Sprintf("panel %q has two targets with refId %q; Grafana silently drops one", label, target.Spec.RefID)})
		}
		seen[target.Spec.RefID] = true
		out = append(out, checkTarget(uid, label, target, datasources)...)
	}
	return out
}

func checkTarget(uid, panelTitle string, target panelQuery, datasources map[string]string) []Violation {
	query := target.Spec.Query
	name := ""
	if query.Datasource != nil {
		name = query.Datasource.Name
	}
	plugin, declared := datasources[name]
	var out []Violation
	if !declared {
		out = append(out, Violation{Resource: uid, Rule: RuleDatasourceRef, Detail: fmt.Sprintf("panel %q: datasource name is %q, want a declared variable reference like ${ds} from profile.MetricsRef()", panelTitle, name)})
	} else if query.Group != "" && plugin != "" && query.Group != plugin {
		out = append(out, Violation{Resource: uid, Rule: RuleDatasourceRef, Detail: fmt.Sprintf("panel %q: query group %q does not match datasource variable plugin %q", panelTitle, query.Group, plugin)})
	}

	expr := query.Spec.Expr
	if strings.TrimSpace(expr) == "" {
		return out
	}
	if err := promql.Check(expr); err != nil {
		out = append(out, Violation{Resource: uid, Rule: RuleQuerySyntax, Detail: fmt.Sprintf("panel %q: %v", panelTitle, err)})
	}
	return append(out, checkExprConventions(uid, panelTitle, expr)...)
}

var rateFamily = regexp.MustCompile(`\b(rate|irate|increase|delta|deriv)\s*\([^)]*\[\s*\d+[smhdwy]\s*\]`)
var dbQueryMetric = regexp.MustCompile(`\bdb_query_\w+`)

func checkExprConventions(uid, panelTitle, expr string) []Violation {
	var out []Violation
	if match := rateFamily.FindString(expr); match != "" {
		out = append(out, Violation{Resource: uid, Rule: RuleRateInterval, Detail: fmt.Sprintf("panel %q: %q uses a fixed range window; use $__rate_interval", panelTitle, match)})
	}
	if match := dbQueryMetric.FindString(expr); match != "" {
		out = append(out, Violation{Resource: uid, Rule: RuleDBNamespace, Detail: fmt.Sprintf("panel %q: %q is semconv's database namespace; RFC-0017 requires pgx_*", panelTitle, match)})
	}
	for _, label := range forbiddenLabels {
		if labelUsed(expr, label) {
			out = append(out, Violation{Resource: uid, Rule: RuleForbiddenLabel, Detail: fmt.Sprintf("panel %q: label %q has unbounded cardinality and RFC-0017 forbids it", panelTitle, label)})
		}
	}
	return out
}

func labelUsed(expr, label string) bool {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(label) + `\b\s*(=~|!~|=|!=)|\b(by|without)\s*\([^)]*\b` + regexp.QuoteMeta(label) + `\b`)
	return pattern.MatchString(expr)
}

func checkLayout(uid string, elements map[string]element, root layout) []Violation {
	var out []Violation
	references := make(map[string]int)
	var walk func(layout)
	walk = func(current layout) {
		for _, row := range current.Spec.Rows {
			walk(row.Spec.Layout)
		}
		items := current.Spec.Items
		for i, item := range items {
			s := item.Spec
			references[s.Element.Name]++
			if _, ok := elements[s.Element.Name]; !ok {
				out = append(out, Violation{Resource: uid, Rule: RuleGridOverlap, Detail: fmt.Sprintf("layout references missing element %q", s.Element.Name)})
			}
			if s.X < 0 || s.Y < 0 || s.Width < 1 || s.Height < 1 || s.X+s.Width > 24 {
				out = append(out, Violation{Resource: uid, Rule: RuleGridOverlap, Detail: fmt.Sprintf("element %q has invalid grid position x=%d y=%d width=%d height=%d", s.Element.Name, s.X, s.Y, s.Width, s.Height)})
			}
			for j := i + 1; j < len(items); j++ {
				a, b := s, items[j].Spec
				if a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height {
					out = append(out, Violation{Resource: uid, Rule: RuleGridOverlap, Detail: fmt.Sprintf("elements %q and %q overlap", a.Element.Name, b.Element.Name)})
				}
			}
		}
	}
	walk(root)
	for key, element := range elements {
		if element.Kind == "Panel" && references[key] != 1 {
			out = append(out, Violation{Resource: uid, Rule: RuleGridOverlap, Detail: fmt.Sprintf("panel element %q has %d layout references, want exactly one", key, references[key])})
		}
	}
	return out
}

// Format renders violations as a sorted, one-per-line report.
func Format(violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}
	lines := make([]string, 0, len(violations))
	for _, violation := range violations {
		lines = append(lines, violation.String())
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}
