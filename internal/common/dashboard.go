// Package common builds the dashboard every board starts from.
//
// One function applies every house default, so a board file states only what
// makes it different. The repo this replaces had no such place, and the result
// was twenty-one boards with schemaVersion spanning 27 to 41, four spellings of
// the datasource variable, and two boards sharing a title.
package common

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

// Tag marks every board this repo generates, so a Grafana search can separate
// generated boards from anything still imported by hand.
const Tag = "obs-as-code"

// NewDashboard returns a dashboard carrying the house defaults, the profile's
// datasource variable, and the board's identity from its registration.
//
// Taking the Meta rather than loose strings means a board cannot declare one uid
// to the registry and build another, and cannot forget the owner tag — which is
// the only place ownership survives now that the output is plain JSON with no
// resource annotations to carry it.
func NewDashboard(p profile.Profile, m registry.Meta, tags ...string) *dashboard.DashboardBuilder {
	all := append([]string{Tag, m.OwnerTag()}, tags...)

	return dashboard.NewDashboardBuilder(m.Title).
		Uid(m.UID).
		Tags(all).
		Refresh("1m").
		Time("now-6h", "now").
		Timezone(common.TimeZoneBrowser).
		// Crosshair rather than tooltip: with several panels stacked, lining up
		// a spike across them is the thing a reader actually does.
		Tooltip(dashboard.DashboardCursorSyncCrosshair).
		Timepicker(dashboard.NewTimePickerBuilder().
			RefreshIntervals([]string{"30s", "1m", "5m", "15m", "1h"})).
		// Editable, because a read-only board pushes people to duplicate it in
		// the UI to explore — and that duplicate is what then rots. The link
		// below tells them where the real source is.
		Editable().
		Link(dashboard.NewDashboardLinkBuilder("Generated from code — edits here are overwritten").
			Type("link").
			Icon("code-branch").
			TargetBlank(true).
			Url(p.SourceURL)).
		WithVariable(p.MetricsVariable())
}
