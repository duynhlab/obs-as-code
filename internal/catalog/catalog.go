// Package catalog explicitly composes every dashboard domain.
package catalog

import (
	"github.com/duynhlab/obs-as-code/internal/dashboards/example"
	"github.com/duynhlab/obs-as-code/internal/dashboards/kubernetes"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

var dashboards = registry.MustNew(append(example.Dashboards(), kubernetes.Dashboards()...)...)

// All returns every dashboard, including non-deployable examples.
func All() []registry.Dashboard { return dashboards.All() }

// Published returns every dashboard written as raw V2 JSON.
func Published() []registry.Dashboard { return dashboards.Published() }

// Deployable returns dashboards wrapped for GrafanaManifest delivery.
func Deployable() []registry.Dashboard { return dashboards.Deployable() }
