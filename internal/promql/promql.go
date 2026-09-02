// Package promql validates the query strings this repo generates.
//
// Two parsers run against every expression, because they answer two different
// questions:
//
//   - VictoriaMetrics/metricsql is the parser of the engine that actually
//     evaluates these queries on the cluster. It answering "yes" means the
//     query will run.
//   - prometheus/promql is the portability gate. Boards here must also work
//     against a plain Prometheus datasource, and MetricsQL is a superset of
//     PromQL — so an expression that only metricsql accepts is one that would
//     break the moment the board is pointed at real Prometheus.
//
// # Grafana macros and variables
//
// Neither parser knows about "$__rate_interval" or "$job", so expressions are
// sanitized first. Macros have known types and are substituted with concrete
// values. Variables are deliberately NOT substituted: instead, a variable is
// only allowed where PromQL already accepts arbitrary text — inside a label
// matcher value, as in {job=~"$job"} — and rejected anywhere else.
//
// The alternative, guessing a replacement for a variable in an arbitrary
// position, cannot be done correctly: `topk($limit, x)` needs a number and
// `sum by ($group) (x)` needs a label name, and a substituter that gets it
// wrong turns this whole gate into a rubber stamp. Constraining where a
// variable may appear makes the check complete instead of approximate, and
// costs nothing: queries in this repo take their selectors as Go values, so
// variables naturally land inside matchers.
package promql

import (
	"errors"
	"fmt"
	"strings"

	"github.com/VictoriaMetrics/metricsql"
	promparser "github.com/prometheus/prometheus/promql/parser"
)

// macros maps every Grafana macro this repo permits to a concrete stand-in of
// the right type. A macro absent from this map is rejected, so a new one has to
// be considered here rather than silently passed to a parser that cannot read
// it.
//
// Order matters: longer keys are substituted first so "$__interval_ms" is not
// eaten by "$__interval".
var macros = []struct{ macro, replacement string }{
	{"$__rate_interval", "5m"},
	{"$__interval_ms", "60000"},
	{"$__interval", "1m"},
	{"$__range_ms", "3600000"},
	{"$__range_s", "3600"},
	{"$__range", "1h"},
	{"$__from", "1735689600000"},
	{"$__to", "1735693200000"},
}

// ErrVariablePosition reports a Grafana variable outside a label matcher value.
var ErrVariablePosition = errors.New("grafana variable outside a label matcher value")

// Sanitize replaces Grafana macros in expr with concrete values so a PromQL
// parser can read it, and reports a variable used somewhere a parser could
// never accept.
func Sanitize(expr string) (string, error) {
	out := expr
	for _, m := range macros {
		out = strings.ReplaceAll(out, m.macro, m.replacement)
	}

	if bad, found := variableOutsideString(out); found {
		return "", fmt.Errorf(
			"%w: %q — put it in a matcher value like {label=~\"%s\"}, or resolve it in Go before it reaches the query",
			ErrVariablePosition, bad, bad)
	}

	return out, nil
}

// variableOutsideString reports the first "$..." occurrence that is not inside a
// double- or single-quoted string, tracking quote state and backslash escapes.
func variableOutsideString(expr string) (string, bool) {
	var quote byte
	escaped := false
	matcherValue := false
	braceDepth := 0

	for i := 0; i < len(expr); i++ {
		c := expr[i]

		if escaped {
			escaped = false
			continue
		}
		switch {
		case quote != 0 && c == '\\':
			escaped = true
		case quote != 0 && c == quote:
			quote = 0
			matcherValue = false
		case quote != 0 && c == '$' && !matcherValue && isGrafanaVariable(expr[i:]):
			return variableToken(expr[i:]), true
		case quote == 0 && (c == '"' || c == '\''):
			quote = c
			matcherValue = braceDepth > 0 && followsMatcherOperator(expr[:i])
		case quote == 0 && c == '{':
			braceDepth++
		case quote == 0 && c == '}' && braceDepth > 0:
			braceDepth--
		case quote == 0 && c == '$' && isGrafanaVariable(expr[i:]):
			return variableToken(expr[i:]), true
		}
	}

	return "", false
}

func isGrafanaVariable(s string) bool {
	if len(s) < 2 || s[0] != '$' {
		return false
	}
	return s[1] == '{' || s[1] == '_' || (s[1] >= 'a' && s[1] <= 'z') || (s[1] >= 'A' && s[1] <= 'Z')
}

func followsMatcherOperator(prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	for _, operator := range []string{"=~", "!~", "!=", "="} {
		if strings.HasSuffix(prefix, operator) {
			return true
		}
	}
	return false
}

// variableToken extracts the variable reference starting at s[0] == '$'.
func variableToken(s string) string {
	if strings.HasPrefix(s, "${") {
		if end := strings.IndexByte(s, '}'); end > 0 {
			return s[:end+1]
		}
		return s
	}

	end := 1
	for end < len(s) && (isAlnum(s[end]) || s[end] == '_') {
		end++
	}
	return s[:end]
}

func isAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// portableParser is stock PromQL: every Options field left false, so an
// expression relying on an experimental Prometheus feature is rejected too.
// promQLParser holds only options and builds a fresh parser per call, so one
// package-level value is safe for concurrent use.
var portableParser = promparser.NewParser(promparser.Options{})

// Validate reports whether expr parses as MetricsQL — that is, whether the
// cluster's engine can run it.
func Validate(expr string) error {
	sanitized, err := Sanitize(expr)
	if err != nil {
		return err
	}
	if _, err := metricsql.Parse(sanitized); err != nil {
		return fmt.Errorf("metricsql: %w (sanitized: %s)", err, sanitized)
	}
	return nil
}

// ValidatePortable reports whether expr also parses as plain PromQL, which is
// what keeps a board usable against a Prometheus datasource rather than only
// against VictoriaMetrics.
func ValidatePortable(expr string) error {
	sanitized, err := Sanitize(expr)
	if err != nil {
		return err
	}
	if _, err := portableParser.ParseExpr(sanitized); err != nil {
		return fmt.Errorf("promql: %w (sanitized: %s) — MetricsQL-only syntax breaks a board pointed at a plain Prometheus", err, sanitized)
	}
	return nil
}

// Check runs both parsers. A query must satisfy both to be used in this repo.
func Check(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("expression is empty")
	}
	if err := Validate(expr); err != nil {
		return err
	}
	return ValidatePortable(expr)
}
