// Package check holds the conformance rules every generated resource must
// satisfy.
//
// The rules run against the rendered JSON rather than against the Go models that
// produced it. That costs an unmarshal and buys two things: the rules validate
// what actually ships, and they keep working when the Foundation SDK — which is
// public preview and says so — reshapes a type.
//
// Every rule here exists because something went wrong before. Overlapping grid
// positions and duplicate refIds are both commits in the history of the repo
// this replaces; the label rules are RFC-0017 made executable; the datasource
// rule is the four hardcoded UIDs found across the twenty-one hand-written
// boards.
package check

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/duynhlab/obs-as-code/internal/promql"
)

// Violation is one broken rule.
type Violation struct {
	// Resource is the UID of the resource at fault.
	Resource string

	// Rule is the short, stable identifier of the rule, so a failure can be
	// grepped for and a suppression discussed by name.
	Rule string

	// Detail says what is wrong and, where it is not obvious, what to do.
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: [%s] %s", v.Resource, v.Rule, v.Detail)
}

// Rule identifiers.
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

// forbiddenLabels are the unbounded-cardinality labels RFC-0017 forbids. A
// single one of these in a query can multiply a metric's series count by the
// number of users or orders in the system; the SDK caps at 2000 attribute sets
// per metric and then silently drops the rest, so the damage is invisible.
var forbiddenLabels = []string{
	"user_id", "userid",
	"order_id", "orderid",
	"session_id", "sessionid",
	"payment_id", "paymentid",
	"promo_code", "promocode",
	"request_id", "requestid",
	"trace_id", "traceid",
	"email", "ip", "ip_address", "remote_addr",
}

// board is the subset of the dashboard model the rules read.
type board struct {
	UID        string  `json:"uid"`
	Title      string  `json:"title"`
	Panels     []panel `json:"panels"`
	Templating struct {
		List []variable `json:"list"`
	} `json:"templating"`
}

type variable struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type panel struct {
	Type    string   `json:"type"`
	Title   string   `json:"title"`
	GridPos gridPos  `json:"gridPos"`
	Targets []target `json:"targets"`
	// Panels holds the children of a collapsed row.
	Panels []panel `json:"panels"`
}

type gridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type target struct {
	Expr       string `json:"expr"`
	RefID      string `json:"refId"`
	Datasource *struct {
		Type string `json:"type"`
		UID  string `json:"uid"`
	} `json:"datasource"`
}

// Dashboard runs every dashboard rule against a rendered model.
func Dashboard(uid string, model []byte) []Violation {
	var b board
	if err := json.Unmarshal(model, &b); err != nil {
		return []Violation{{Resource: uid, Rule: RuleQuerySyntax, Detail: "model is not valid JSON: " + err.Error()}}
	}

	var out []Violation
	out = append(out, checkDatasourceVariable(uid, b)...)
	out = append(out, checkPanels(uid, flatten(b.Panels))...)
	out = append(out, checkGrid(uid, flatten(b.Panels))...)
	return out
}

// flatten returns every panel including those nested inside collapsed rows, but
// not the row panels themselves — a row legitimately has no query and no size.
func flatten(panels []panel) []panel {
	out := make([]panel, 0, len(panels))
	for _, p := range panels {
		if p.Type == "row" {
			out = append(out, flatten(p.Panels)...)
			continue
		}
		out = append(out, p)
	}
	return out
}

func checkDatasourceVariable(uid string, b board) []Violation {
	for _, v := range b.Templating.List {
		if v.Type == "datasource" {
			return nil
		}
	}
	// Panels reference the datasource as "${ds}". Without the variable that
	// resolves to nothing and every panel renders empty — with no error.
	return []Violation{{
		Resource: uid,
		Rule:     RuleNoDatasourceVar,
		Detail:   "board declares no datasource variable; build it with common.NewDashboard",
	}}
}

func checkPanels(uid string, panels []panel) []Violation {
	var out []Violation

	for _, p := range panels {
		label := p.Title
		if label == "" {
			label = "(untitled " + p.Type + ")"
			out = append(out, Violation{
				Resource: uid, Rule: RulePanelNoTitle,
				Detail: "a " + p.Type + " panel has no title",
			})
		}

		if len(p.Targets) == 0 {
			out = append(out, Violation{
				Resource: uid, Rule: RulePanelNoTarget,
				Detail: fmt.Sprintf("panel %q has no query, so it renders empty", label),
			})
			continue
		}

		seen := make(map[string]bool, len(p.Targets))
		for _, t := range p.Targets {
			// Two targets sharing a refId: Grafana keeps one and drops the
			// other without reporting anything. This is a real commit in the
			// previous repo's history.
			if t.RefID != "" {
				if seen[t.RefID] {
					out = append(out, Violation{
						Resource: uid, Rule: RuleDuplicateRefID,
						Detail: fmt.Sprintf("panel %q has two targets with refId %q; Grafana silently drops one", label, t.RefID),
					})
				}
				seen[t.RefID] = true
			}

			out = append(out, checkTarget(uid, label, t)...)
		}
	}

	return out
}

func checkTarget(uid, panelTitle string, t target) []Violation {
	var out []Violation

	if t.Datasource == nil || !strings.HasPrefix(t.Datasource.UID, "${") {
		got := "absent"
		if t.Datasource != nil {
			got = t.Datasource.UID
		}
		out = append(out, Violation{
			Resource: uid, Rule: RuleDatasourceRef,
			Detail: fmt.Sprintf("panel %q: datasource uid is %q, want a variable reference like ${ds} — take it from profile.MetricsRef()", panelTitle, got),
		})
	}

	if strings.TrimSpace(t.Expr) == "" {
		return out
	}

	if err := promql.Check(t.Expr); err != nil {
		out = append(out, Violation{
			Resource: uid, Rule: RuleQuerySyntax,
			Detail: fmt.Sprintf("panel %q: %v", panelTitle, err),
		})
	}
	out = append(out, checkExprConventions(uid, panelTitle, t.Expr)...)

	return out
}

// rateFamily matches a rate-style function applied to a literal range window.
var rateFamily = regexp.MustCompile(`\b(rate|irate|increase|delta|deriv)\s*\([^)]*\[\s*\d+[smhdwy]\s*\]`)

// dbQueryMetric matches semconv's database metric namespace.
var dbQueryMetric = regexp.MustCompile(`\bdb_query_\w+`)

func checkExprConventions(uid, panelTitle, expr string) []Violation {
	var out []Violation

	if m := rateFamily.FindString(expr); m != "" {
		out = append(out, Violation{
			Resource: uid, Rule: RuleRateInterval,
			Detail: fmt.Sprintf("panel %q: %q uses a fixed range window; use $__rate_interval so the query still returns data when the panel is zoomed out", panelTitle, m),
		})
	}

	if m := dbQueryMetric.FindString(expr); m != "" {
		out = append(out, Violation{
			Resource: uid, Rule: RuleDBNamespace,
			Detail: fmt.Sprintf("panel %q: %q is semconv's database namespace; RFC-0017 puts database metrics in pgx_*", panelTitle, m),
		})
	}

	for _, label := range forbiddenLabels {
		if !labelUsed(expr, label) {
			continue
		}
		out = append(out, Violation{
			Resource: uid, Rule: RuleForbiddenLabel,
			Detail: fmt.Sprintf("panel %q: label %q has unbounded cardinality and RFC-0017 forbids it", panelTitle, label),
		})
	}

	return out
}

// labelUsed reports whether expr references label as a label name — in a
// matcher, or in a by/without clause — rather than merely containing the word.
func labelUsed(expr, label string) bool {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(label) + `\b\s*(=~|!~|=|!=)|\b(by|without)\s*\([^)]*\b` + regexp.QuoteMeta(label) + `\b`)
	return pattern.MatchString(expr)
}

func checkGrid(uid string, panels []panel) []Violation {
	var out []Violation

	for i := range panels {
		for j := i + 1; j < len(panels); j++ {
			a, b := panels[i].GridPos, panels[j].GridPos
			if a.W == 0 || a.H == 0 || b.W == 0 || b.H == 0 {
				continue
			}
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				out = append(out, Violation{
					Resource: uid, Rule: RuleGridOverlap,
					Detail: fmt.Sprintf("panels %q and %q overlap at (%d,%d) and (%d,%d); set layout with .Span()/.Height() and let the SDK compute gridPos",
						panels[i].Title, panels[j].Title, a.X, a.Y, b.X, b.Y),
				})
			}
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
	for _, v := range violations {
		lines = append(lines, v.String())
	}
	slices.Sort(lines)

	return strings.Join(lines, "\n")
}
