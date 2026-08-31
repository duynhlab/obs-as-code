package render

import (
	"fmt"

	"github.com/duynhlab/obs-as-code/internal/folders"
	"github.com/duynhlab/obs-as-code/internal/profile"
)

// grafanaFolder is the subset of GrafanaFolder this repo emits.
type grafanaFolder struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   objectMeta        `json:"metadata"`
	Spec       grafanaFolderSpec `json:"spec"`
}

type grafanaFolderSpec struct {
	commonSpec

	// CustomUID is spec.uid. The CRD marks it immutable, so a rename is a
	// delete and recreate — of the folder and of everything filed under it.
	CustomUID string `json:"uid,omitempty"`

	Title string `json:"title,omitempty"`

	ParentFolderUID string `json:"parentFolderUID,omitempty"`
}

// Folder renders f as a GrafanaFolder resource.
func Folder(p profile.Profile, f folders.Folder) (Object, error) {
	if err := p.Validate(); err != nil {
		return Object{}, err
	}
	if err := f.Validate(); err != nil {
		return Object{}, fmt.Errorf("render folder: %w", err)
	}

	const kind = "GrafanaFolder"

	return Object{
		Kind:      kind,
		Name:      f.UID,
		Namespace: p.Namespace,
		body: grafanaFolder{
			APIVersion: APIVersion,
			Kind:       kind,
			Metadata:   meta(p, f.UID, ""),
			Spec: grafanaFolderSpec{
				commonSpec:      common(p),
				CustomUID:       f.UID,
				Title:           f.Title,
				ParentFolderUID: f.ParentUID,
			},
		},
	}, nil
}

// meta builds the metadata every generated resource carries. The labels let an
// operator answer "what put this here" from `kubectl get -l`, and the owner
// annotation answers "who do I ask about it" without opening the repo.
func meta(p profile.Profile, name, owner string) objectMeta {
	m := objectMeta{
		Name:      name,
		Namespace: p.Namespace,
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "obs-as-code",
			"obs-as-code/profile":          p.Name,
		},
		Annotations: map[string]string{
			"obs-as-code/source": p.SourceURL,
		},
	}
	if owner != "" {
		m.Annotations["obs-as-code/owner"] = owner
	}
	return m
}

// common builds GrafanaCommonSpec from the profile.
func common(p profile.Profile) commonSpec {
	return commonSpec{
		ResyncPeriod:     p.ResyncPeriod.String(),
		InstanceSelector: labelSelector{MatchLabels: p.InstanceLabels},
		// The operator runs with watchNamespaces=monitoring, and every
		// existing resource in the cluster sets this. Leaving it off makes a
		// resource in any other namespace invisible with no error reported.
		AllowCrossNamespaceImport: true,
	}
}
