// Package render provides the canonical JSON encoding used by generated files
// and golden tests.
package render

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/resource"
)

// MaxModelBytes is the browser-oriented size budget for one dashboard resource.
const MaxModelBytes = 512 * 1024

// DashboardJSON renders a dashboard API resource with stable indentation.
func DashboardJSON(manifest resource.Manifest) ([]byte, error) {
	out, err := JSON(manifest)
	if err != nil {
		return nil, err
	}
	if len(out) > MaxModelBytes {
		return nil, fmt.Errorf("render: dashboard %q is %d bytes, budget is %d — split it", manifest.Metadata.Name, len(out), MaxModelBytes)
	}
	return out, nil
}

// JSON renders any generated resource with stable indentation and a trailing newline.
func JSON(value any) ([]byte, error) {
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render: marshal JSON: %w", err)
	}
	return append(out, '\n'), nil
}
