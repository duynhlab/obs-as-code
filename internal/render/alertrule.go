package render

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-foundation-sdk/go/alerting"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/naming"
	"github.com/duynhlab/obs-as-code/internal/profile"
)

// The GrafanaAlertRuleGroup CRD and the Foundation SDK's alerting types are
// close but not identical, so this file is a real translation rather than a
// pass-through. The differences, read from operator 5.24.0's
// api/v1beta1/grafanaalertrulegroup_types.go against SDK v0.0.18:
//
//   - the SDK writes the rule's notification settings under the JSON key
//     "notification_settings"; the CRD reads "notificationSettings"
//   - the SDK's KeepFiringFor is an int64 count of nanoseconds; the CRD wants a
//     duration string matching ^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$
//   - the CRD requires a per-rule uid; the SDK's is optional
//   - the SDK carries FolderUID, RuleGroup and OrgID on each rule, which Grafana
//     itself populates. Sending them in a resource is at best redundant and at
//     worst contradicts the group the resource declares, so they are dropped
//
// Getting any of these wrong produces a resource the API server accepts and the
// operator then mis-syncs, which is why the mapping is spelled out field by
// field instead of being inferred by re-marshalling.

type grafanaAlertRuleGroup struct {
	APIVersion string                    `json:"apiVersion"`
	Kind       string                    `json:"kind"`
	Metadata   objectMeta                `json:"metadata"`
	Spec       grafanaAlertRuleGroupSpec `json:"spec"`
}

type grafanaAlertRuleGroupSpec struct {
	commonSpec

	Name      string `json:"name,omitempty"`
	FolderRef string `json:"folderRef,omitempty"`

	// Interval is required by the CRD and must be a duration string.
	Interval string `json:"interval"`

	// Rules is required with MinItems=1.
	Rules []alertRule `json:"rules"`

	Editable *bool `json:"editable,omitempty"`
}

// alertRule mirrors the CRD's AlertRule, not the SDK's Rule.
type alertRule struct {
	UID           string                `json:"uid"`
	Title         string                `json:"title"`
	Condition     string                `json:"condition"`
	Data          []alertQuery          `json:"data"`
	For           string                `json:"for"`
	NoDataState   string                `json:"noDataState"`
	ExecErrState  string                `json:"execErrState"`
	Labels        map[string]string     `json:"labels,omitempty"`
	Annotations   map[string]string     `json:"annotations,omitempty"`
	IsPaused      bool                  `json:"isPaused,omitempty"`
	KeepFiringFor string                `json:"keepFiringFor,omitempty"`
	Notification  *notificationSettings `json:"notificationSettings,omitempty"`
}

type alertQuery struct {
	RefID             string             `json:"refId,omitempty"`
	DatasourceUID     string             `json:"datasourceUid,omitempty"`
	QueryType         string             `json:"queryType,omitempty"`
	RelativeTimeRange *relativeTimeRange `json:"relativeTimeRange,omitempty"`
	Model             json.RawMessage    `json:"model,omitempty"`
}

type relativeTimeRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// notificationSettings mirrors the CRD's NotificationSettings. Its inner keys
// are snake_case in both models; only the parent key differs.
type notificationSettings struct {
	Receiver      string   `json:"receiver"`
	GroupBy       []string `json:"group_by,omitempty"`
	GroupWait     string   `json:"group_wait,omitempty"`
	GroupInterval string   `json:"group_interval,omitempty"`
}

// AlertRuleGroupInput is everything render needs to emit one rule group.
type AlertRuleGroupInput struct {
	// UID is the resource name.
	UID string

	// Folder is where the group's rules are filed.
	Folder folders.Folder

	// Owner is the team or person paged by these rules.
	Owner string

	// Group is the built SDK rule group.
	Group alerting.RuleGroup
}

// AlertRuleGroup renders in as a GrafanaAlertRuleGroup resource.
//
// Nothing registers an alert group yet. This exists and is tested now so that
// the first group to be written finds the translation above already correct,
// rather than discovering it against a live Alertmanager.
func AlertRuleGroup(p profile.Profile, in AlertRuleGroupInput) (Object, error) {
	if err := p.Validate(); err != nil {
		return Object{}, err
	}
	if err := naming.Validate("alert rule group", in.UID); err != nil {
		return Object{}, fmt.Errorf("render alert rule group: %w", err)
	}
	if err := in.Folder.Validate(); err != nil {
		return Object{}, fmt.Errorf("render alert rule group %q: %w", in.UID, err)
	}
	if len(in.Group.Rules) == 0 {
		return Object{}, fmt.Errorf("render alert rule group %q: no rules; the CRD requires at least one", in.UID)
	}
	if in.Group.Interval == nil || *in.Group.Interval <= 0 {
		return Object{}, fmt.Errorf("render alert rule group %q: interval is required and must be positive", in.UID)
	}

	rules := make([]alertRule, 0, len(in.Group.Rules))
	for i, r := range in.Group.Rules {
		converted, err := convertRule(r)
		if err != nil {
			return Object{}, fmt.Errorf("render alert rule group %q: rule %d: %w", in.UID, i, err)
		}
		rules = append(rules, converted)
	}

	title := in.UID
	if in.Group.Title != nil && *in.Group.Title != "" {
		title = *in.Group.Title
	}

	const kind = "GrafanaAlertRuleGroup"

	return Object{
		Kind:      kind,
		Name:      in.UID,
		Namespace: p.Namespace,
		body: grafanaAlertRuleGroup{
			APIVersion: APIVersion,
			Kind:       kind,
			Metadata:   meta(p, in.UID, in.Owner),
			Spec: grafanaAlertRuleGroupSpec{
				commonSpec: common(p),
				Name:       title,
				FolderRef:  in.Folder.UID,
				// RuleGroup.Interval is documented as seconds.
				Interval: (time.Duration(*in.Group.Interval) * time.Second).String(),
				Rules:    rules,
			},
		},
	}, nil
}

func convertRule(r alerting.Rule) (alertRule, error) {
	if r.Uid == nil || *r.Uid == "" {
		return alertRule{}, fmt.Errorf("uid is required by the CRD but the SDK leaves it optional; set it explicitly")
	}
	if err := naming.Validate("alert rule", *r.Uid); err != nil {
		return alertRule{}, err
	}
	if r.Title == "" {
		return alertRule{}, fmt.Errorf("title is empty")
	}
	if r.Condition == "" {
		return alertRule{}, fmt.Errorf("condition is empty")
	}

	refIDs := make(map[string]bool, len(r.Data))
	data := make([]alertQuery, 0, len(r.Data))
	for _, q := range r.Data {
		converted, err := convertQuery(q)
		if err != nil {
			return alertRule{}, err
		}
		refIDs[converted.RefID] = true
		data = append(data, converted)
	}
	// A condition naming a refId that no query produces evaluates to nothing
	// and reports no error, so the rule silently never fires.
	if !refIDs[r.Condition] {
		return alertRule{}, fmt.Errorf("condition %q names no query in data", r.Condition)
	}

	out := alertRule{
		UID:          *r.Uid,
		Title:        r.Title,
		Condition:    r.Condition,
		Data:         data,
		For:          r.For,
		NoDataState:  string(r.NoDataState),
		ExecErrState: string(r.ExecErrState),
		Labels:       r.Labels,
		Annotations:  r.Annotations,
	}
	if r.IsPaused != nil {
		out.IsPaused = *r.IsPaused
	}
	if r.KeepFiringFor != nil {
		// SDK: nanoseconds as int64. CRD: duration string.
		out.KeepFiringFor = time.Duration(*r.KeepFiringFor).String()
	}
	if r.NotificationSettings != nil {
		out.Notification = convertNotification(*r.NotificationSettings)
	}

	return out, nil
}

func convertQuery(q alerting.Query) (alertQuery, error) {
	if q.RefId == nil || *q.RefId == "" {
		return alertQuery{}, fmt.Errorf("query has no refId")
	}

	out := alertQuery{RefID: *q.RefId}
	if q.DatasourceUid != nil {
		out.DatasourceUID = *q.DatasourceUid
	}
	if q.QueryType != nil {
		out.QueryType = *q.QueryType
	}
	if r := q.RelativeTimeRange; r != nil {
		out.RelativeTimeRange = &relativeTimeRange{}
		if r.From != nil {
			out.RelativeTimeRange.From = int64(*r.From)
		}
		if r.To != nil {
			out.RelativeTimeRange.To = int64(*r.To)
		}
	}
	if q.Model != nil {
		model, err := json.Marshal(q.Model)
		if err != nil {
			return alertQuery{}, fmt.Errorf("query %q: marshal model: %w", out.RefID, err)
		}
		out.Model = model
	}

	return out, nil
}

func convertNotification(n alerting.NotificationSettings) *notificationSettings {
	out := &notificationSettings{
		Receiver: n.Receiver,
		GroupBy:  n.GroupBy,
	}
	if n.GroupWait != nil {
		out.GroupWait = *n.GroupWait
	}
	if n.GroupInterval != nil {
		out.GroupInterval = *n.GroupInterval
	}
	return out
}
