// Package common builds the dashboard every board starts from.
//
// One function applies every house default, so a board file states only what
// makes it different. The previous repo had no such place, and the result was
// twenty-one boards with schemaVersion spanning 27 to 41, four different
// spellings of the datasource variable, and two boards sharing a title.
package common

import (
	"github.com/grafana/grafana-foundation-sdk/go/common"
	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/profile"
)

// Tag marks every board this repo generates, so a Grafana search can separate
// generated boards from anything still imported by hand.
const Tag = "obs-as-code"

// NewDashboard returns a dashboard carrying the house defaults and the
// profile's datasource variable.
//
// The variable is not optional: panels reference the datasource as
// "${<MetricsVar>}", so a board without it renders every panel empty.
func NewDashboard(p profile.Profile, uid, title string, tags ...string) *dashboard.DashboardBuilder {
	return dashboard.NewDashboardBuilder(title).
		Uid(uid).
		Tags(append([]string{Tag}, tags...)).
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
