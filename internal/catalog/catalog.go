// Package catalog is the single place that compiles resources into the binary.
//
// Every dashboard and alert package is imported here for its registration side
// effect, and nothing else imports them. That means adding a resource is one
// new file plus one line here, and it means the conformance suite and the
// generator see exactly the same set — a resource cannot be tested but not
// shipped, or shipped but not tested.
package catalog

import (
	_ "github.com/duynhlab/obs-as-code/internal/alerts/example"
	_ "github.com/duynhlab/obs-as-code/internal/dashboards/example"
)
