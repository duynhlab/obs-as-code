// Package catalog is the single place that compiles resources into the binary.
//
// Every dashboard package is imported here for its registration side effect, and
// nothing else imports them. Adding a board is one new file plus one line here,
// and the conformance suite and the generator see exactly the same set — a board
// cannot be tested but not shipped, or shipped but not tested.
package catalog

import (
	_ "github.com/duynhlab/obs-as-code/internal/dashboards/example"
	_ "github.com/duynhlab/obs-as-code/internal/dashboards/kubernetes"
)
