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
	// RuleDatasourceVarValue exists because RuleDatasourceRef only proves a
	// panel points at a DECLARED variable, never that the variable points at
	// something Grafana can resolve. A variable whose current.value held a
	// display name instead of a uid passed every other rule here while making
	// every panel on every board render empty.
	RuleDatasourceVarValue = "datasource-var-value"
	// RuleQueryVarValue exists because V2's QueryVariableSpec.Current has no
	// omitempty: a variable nobody set serialises as {"text":"","value":""},
	// and Grafana honours that as a real selection of the empty string rather
	// than "nothing chosen yet". Every panel filtering on such a variable
	// matches nothing. Classic dashboards emitted current: null and Grafana
	// resolved it from the options, which is why the pre-V2 boards kept working
	// through the same migration that blanked these.
	RuleQueryVarValue = "query-var-value"
	// RuleVariableQuery exists because promql.Check had exactly one non-test
	// call site, inside checkTarget: panel queries were gated and variable
	// queries were not, while both defects that shipped an unusable board lived
	// in variables. A variable whose selector is malformed or empty yields an
	// empty dropdown, and an empty dropdown means every panel filtering on it
	// matches nothing — the same silent emptiness, one layer earlier.
	RuleVariableQuery  = "variable-query"
	RuleQuerySyntax    = "query-syntax"
	RuleRateInterval   = "rate-interval"
	RuleForbiddenLabel = "forbidden-label"
	RuleDBNamespace    = "db-namespace"
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
		// Current is decoded loosely on purpose: the V2 schema allows a string
		// or an array of strings here, and a wrong Go type would make this
		// check silently skip rather than fail.
		Current *struct {
			Value any `json:"value"`
		} `json:"current"`
		Query dataQuery `json:"query"`
	} `json:"spec"`
}

// dataQuery is the same wire shape whether it backs a panel target or a
// variable, which is why one type serves both — and a reminder that a rule
// applying to one applies to the other unless there is a reason it does not.
type dataQuery struct {
	Group      string `json:"group"`
	Datasource *struct {
		Name string `json:"name"`
	} `json:"datasource"`
	Spec struct {
		Expr string `json:"expr"`
	} `json:"spec"`
}

// datasourceUID is the shape Grafana accepts as a datasource uid: lowercase
// alphanumerics and dashes. A display name — capitals, spaces, parentheses —
// cannot match, which is the whole point.
var datasourceUID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

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
		RefID string    `json:"refId"`
		Query dataQuery `json:"query"`
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
	var out []Violation
	for _, variable := range board.Spec.Variables {
		if variable.Kind == "QueryVariable" {
			out = append(out, checkQueryVariable(uid, variable)...)
			out = append(out, checkVariableQuery(uid, variable)...)
			continue
		}
		if variable.Kind != "DatasourceVariable" || variable.Spec.Name == "" {
			continue
		}
		datasources["${"+variable.Spec.Name+"}"] = variable.Spec.PluginID

		// An absent current is fine — the board defers to Grafana's default.
		// A present one must be a uid, because ${var} expands to this value and
		// Grafana resolves datasources by uid alone.
		if variable.Spec.Current == nil {
			continue
		}
		value, ok := variable.Spec.Current.Value.(string)
		if !ok {
			out = append(out, Violation{Resource: uid, Rule: RuleDatasourceVarValue,
				Detail: fmt.Sprintf("variable %q has a non-string current.value (%T); a datasource variable resolves one uid", variable.Spec.Name, variable.Spec.Current.Value)})
			continue
		}
		if !datasourceUID.MatchString(value) {
			out = append(out, Violation{Resource: uid, Rule: RuleDatasourceVarValue,
				Detail: fmt.Sprintf("variable %q has current.value %q, which is not a datasource uid; use the uid, not the display name", variable.Spec.Name, value)})
		}
	}
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

// checkQueryVariable rejects a variable that ships no selection. See
// RuleQueryVarValue for why an unset Current is not merely cosmetic.
func checkQueryVariable(uid string, v variable) []Violation {
	name := v.Spec.Name
	if v.Spec.Current == nil {
		return []Violation{{Resource: uid, Rule: RuleQueryVarValue,
			Detail: fmt.Sprintf("query variable %q has no current selection, so every panel filtering on it matches nothing", name)}}
	}
	if selectsSomething(v.Spec.Current.Value) {
		return nil
	}
	return []Violation{{Resource: uid, Rule: RuleQueryVarValue,
		Detail: fmt.Sprintf("query variable %q selects nothing (current.value is %#v); wrap the builder in common.SelectAll or set a concrete default", name, v.Spec.Current.Value)}}
}

// selectsSomething reports whether a current.value names at least one option.
// Multi-select stores an array, so a bare string check would call every
// multi-value variable broken.
func selectsSomething(value any) bool {
	switch v := value.(type) {
	case string:
		return v != ""
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				return true
			}
		}
		return false
	default:
		return false
	}
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

	// A blank expression used to return early here, which meant promql.Check
	// never ran and RulePanelNoTarget could not fire either, because the target
	// existed — it just had nothing in it. Every gate passed and the panel
	// rendered empty.
	expr := query.Spec.Expr
	if strings.TrimSpace(expr) == "" {
		return append(out, Violation{Resource: uid, Rule: RulePanelNoTarget,
			Detail: fmt.Sprintf("panel %q has a target with an empty expression, so it renders empty", panelTitle)})
	}
	subject := fmt.Sprintf("panel %q", panelTitle)
	if err := promql.Check(expr); err != nil {
		out = append(out, Violation{Resource: uid, Rule: RuleQuerySyntax, Detail: fmt.Sprintf("%s: %v", subject, err)})
	}
	return append(out, checkExprConventions(uid, subject, expr)...)
}

// labelValuesCall unwraps Grafana's label_values(<selector>, <label>).
//
// The wrapper is a datasource function rather than PromQL, so promql.Check
// rejects a whole variable query outright — measured on both of this repo's
// real expressions. The selector inside it is PromQL, and it is the part the
// datasource evaluates, so that is what gets checked.
var labelValuesCall = regexp.MustCompile(`^label_values\((.*),\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\)$`)

// checkVariableQuery gates the PromQL a variable is built from.
//
// An expression this cannot unwrap is checked as plain PromQL, which is what a
// variable query is when it uses no datasource function. An author reaching for
// a different one (query_result, metrics) will see this rule fire and must
// extend labelValuesCall — failing loudly beats silently trusting a shape no
// rule understands, which is how the gate came to skip variables in the first
// place.
func checkVariableQuery(uid string, v variable) []Violation {
	subject := fmt.Sprintf("variable %q", v.Spec.Name)
	expr := strings.TrimSpace(v.Spec.Query.Spec.Expr)
	if expr == "" {
		return []Violation{{Resource: uid, Rule: RuleVariableQuery,
			Detail: fmt.Sprintf("%s has no query expression, so its dropdown is empty and every panel filtering on it matches nothing", subject)}}
	}

	inner := expr
	if match := labelValuesCall.FindStringSubmatch(expr); match != nil {
		inner = strings.TrimSpace(match[1])
		if inner == "" {
			return []Violation{{Resource: uid, Rule: RuleVariableQuery,
				Detail: fmt.Sprintf("%s wraps an empty series selector in label_values(), which lists nothing", subject)}}
		}
	}

	var out []Violation
	if err := promql.Check(inner); err != nil {
		out = append(out, Violation{Resource: uid, Rule: RuleVariableQuery,
			Detail: fmt.Sprintf("%s: %v", subject, err)})
	}
	return append(out, checkExprConventions(uid, subject, inner)...)
}

var rateFamily = regexp.MustCompile(`\b(rate|irate|increase|delta|deriv)\s*\([^)]*\[\s*\d+[smhdwy]\s*\]`)
var dbQueryMetric = regexp.MustCompile(`\bdb_query_\w+`)

// checkExprConventions takes a pre-formatted subject (`panel "X"`, `variable
// "y"`) because these conventions hold wherever PromQL appears, and a rule that
// can only describe panels invites a second copy for variables.
func checkExprConventions(uid, subject, expr string) []Violation {
	var out []Violation
	if match := rateFamily.FindString(expr); match != "" {
		out = append(out, Violation{Resource: uid, Rule: RuleRateInterval, Detail: fmt.Sprintf("%s: %q uses a fixed range window; use $__rate_interval", subject, match)})
	}
	if match := dbQueryMetric.FindString(expr); match != "" {
		out = append(out, Violation{Resource: uid, Rule: RuleDBNamespace, Detail: fmt.Sprintf("%s: %q is semconv's database namespace; RFC-0017 requires pgx_*", subject, match)})
	}
	for _, label := range forbiddenLabels {
		if labelUsed(expr, label) {
			out = append(out, Violation{Resource: uid, Rule: RuleForbiddenLabel, Detail: fmt.Sprintf("%s: label %q has unbounded cardinality and RFC-0017 forbids it", subject, label)})
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
