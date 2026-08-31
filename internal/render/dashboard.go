package render

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-foundation-sdk/go/dashboard"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/naming"
	"github.com/duynhlab/obs-as-code/internal/profile"
)

// MaxObjectBytes is the size budget for one rendered resource.
//
// etcd rejects objects over roughly 1 MiB. Half of that is the budget here: a
// board that needs more is a board that should be split, and if one genuinely
// cannot be, GrafanaDashboard.spec.oci keeps the model out of etcd entirely.
// Failing at 512 KiB leaves room to notice before the hard wall.
const MaxObjectBytes = 512 * 1024

// grafanaDashboard is the subset of GrafanaDashboard this repo emits.
type grafanaDashboard struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   objectMeta           `json:"metadata"`
	Spec       grafanaDashboardSpec `json:"spec"`
}

type grafanaDashboardSpec struct {
	commonSpec

	// CustomUID is spec.uid, which overrides the uid inside the model. The CRD
	// marks it immutable: renaming a board means deleting the resource first.
	CustomUID string `json:"uid,omitempty"`

	// GzipJSON carries the model. encoding/json renders a []byte as base64,
	// which is exactly what the CRD asks for.
	GzipJSON []byte `json:"gzipJson,omitempty"`

	// FolderRef names a GrafanaFolder resource in the same namespace. Preferred
	// over the plain `folder` title string because it makes the dependency on a
	// folder resource explicit, and over folderUID because the operator then
	// resolves the UID from a resource this repo also generates. The CRD
	// permits only one of the three.
	FolderRef string `json:"folderRef,omitempty"`
}

// DashboardInput is everything render needs to emit one dashboard resource.
type DashboardInput struct {
	// UID is the Grafana UID and the resource name.
	UID string

	// Folder is where the board is filed.
	Folder folders.Folder

	// Owner is the team or person to ask about this board.
	Owner string

	// Model is the dashboard JSON, as produced by DashboardJSON. Passing bytes
	// rather than the model means the resource and any golden file or
	// standalone JSON output are guaranteed to carry the same bytes.
	Model []byte
}

// DashboardJSON is the one canonical marshalling of a built dashboard.
//
// Every consumer — the resource body, the reviewable JSON written beside it,
// the golden files — goes through here, so none of them can disagree about what
// the board actually is.
func DashboardJSON(d dashboard.Dashboard) ([]byte, error) {
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("render: marshal dashboard model: %w", err)
	}
	return append(out, '\n'), nil
}

// Dashboard renders in as a GrafanaDashboard resource.
func Dashboard(p profile.Profile, in DashboardInput) (Object, error) {
	if err := p.Validate(); err != nil {
		return Object{}, err
	}
	if err := naming.Validate("dashboard", in.UID); err != nil {
		return Object{}, fmt.Errorf("render dashboard: %w", err)
	}
	if err := in.Folder.Validate(); err != nil {
		return Object{}, fmt.Errorf("render dashboard %q: %w", in.UID, err)
	}
	if len(in.Model) == 0 {
		return Object{}, fmt.Errorf("render dashboard %q: model is empty", in.UID)
	}

	gz, err := gzipJSON(in.Model)
	if err != nil {
		return Object{}, fmt.Errorf("render dashboard %q: %w", in.UID, err)
	}

	const kind = "GrafanaDashboard"

	obj := Object{
		Kind:      kind,
		Name:      in.UID,
		Namespace: p.Namespace,
		body: grafanaDashboard{
			APIVersion: APIVersion,
			Kind:       kind,
			Metadata:   meta(p, in.UID, in.Owner),
			Spec: grafanaDashboardSpec{
				commonSpec: common(p),
				CustomUID:  in.UID,
				GzipJSON:   gz,
				FolderRef:  in.Folder.UID,
			},
		},
	}

	// Checked here rather than in a test so a board that outgrows etcd fails
	// the generate that produced it, not an apply hours later.
	y, err := obj.YAML()
	if err != nil {
		return Object{}, err
	}
	if len(y) > MaxObjectBytes {
		return Object{}, fmt.Errorf(
			"render dashboard %q: rendered resource is %d bytes, budget is %d — split the board, or move the model out of etcd with spec.oci",
			in.UID, len(y), MaxObjectBytes)
	}

	return obj, nil
}
