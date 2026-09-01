// Package render turns a built dashboard into the bytes that ship.
//
// One function does the marshalling, so the JSON written to disk, the JSON in a
// golden file and the JSON anyone imports are the same bytes by construction
// rather than by coincidence.
package render

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

// MaxModelBytes is the size budget for one dashboard.
//
// Not an etcd limit: the model is delivered through an OCI artifact and never
// enters etcd. This is about the browser. A dashboard this large takes seconds
// to parse and render, and is almost always several boards wearing a trench
// coat — the fix is to split it, not to raise the number.
const MaxModelBytes = 512 * 1024

// DashboardJSON is the one canonical marshalling of a built dashboard.
func DashboardJSON(d dashboard.Dashboard) ([]byte, error) {
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render: marshal dashboard model: %w", err)
	}
	out = append(out, '\n')

	uid := "(no uid)"
	if d.Uid != nil {
		uid = *d.Uid
	}
	if len(out) > MaxModelBytes {
		return nil, fmt.Errorf(
			"render: dashboard %q is %d bytes, budget is %d — split it; a board this size is slow to render and is usually several boards in one",
			uid, len(out), MaxModelBytes)
	}

	return out, nil
}
