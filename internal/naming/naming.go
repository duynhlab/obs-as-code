// Package naming holds the one rule for identifiers this repo generates.
//
// A Grafana UID we choose is used twice: as spec.uid in a Grafana Operator
// resource, and verbatim as that resource's metadata.name. The two have
// different constraints, and the CRD only enforces its own:
//
//	CRD spec.uid:      ^[a-zA-Z0-9-_]+$   MaxLength 40
//	Kubernetes name:   DNS-1123 label — lowercase alphanumeric and '-'
//
// A UID like "My_Board" satisfies the CRD and is rejected by the API server, so
// the rule below is the intersection of both. Enforcing the intersection also
// makes kebab-case the only spelling available, which is why no separate
// "use kebab-case" convention has to be remembered.
package naming

import (
	"fmt"
	"regexp"
)

// MaxLen is the MaxLength every Grafana Operator CRD puts on spec.uid.
const MaxLen = 40

// pattern is a DNS-1123 label, which is also a strict subset of the CRD's own
// allowed character set.
var pattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Validate reports whether id can be used as both a Grafana UID and a
// Kubernetes resource name. kind names the thing being validated so the error
// says what to go fix.
func Validate(kind, id string) error {
	switch {
	case id == "":
		return fmt.Errorf("%s: uid is empty", kind)
	case len(id) > MaxLen:
		return fmt.Errorf("%s %q: uid is %d chars, the CRD allows %d", kind, id, len(id), MaxLen)
	case !pattern.MatchString(id):
		return fmt.Errorf("%s %q: uid must be a DNS-1123 label (lowercase letters, digits and '-', not starting or ending with '-'), matching %s", kind, id, pattern)
	default:
		return nil
	}
}

// IsValid reports whether id satisfies Validate.
func IsValid(id string) bool { return Validate("", id) == nil }
