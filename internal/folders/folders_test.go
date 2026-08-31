package folders_test

import (
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/folders"
)

func TestAllAreValidAndUnique(t *testing.T) {
	t.Parallel()

	seenUID := make(map[string]string)
	seenTitle := make(map[string]string)

	for _, f := range folders.All() {
		t.Run(f.UID, func(t *testing.T) {
			t.Parallel()

			if err := f.Validate(); err != nil {
				t.Errorf("Validate() = %v", err)
			}
		})

		// A duplicate UID would make two folders fight over one Grafana
		// folder; a duplicate title makes them indistinguishable in the UI.
		if other, dup := seenUID[f.UID]; dup {
			t.Errorf("uid %q used by both %q and %q", f.UID, other, f.Title)
		}
		seenUID[f.UID] = f.Title

		if other, dup := seenTitle[f.Title]; dup {
			t.Errorf("title %q used by both uid %q and %q", f.Title, other, f.UID)
		}
		seenTitle[f.Title] = f.UID
	}
}

func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	first := folders.All()
	if len(first) == 0 {
		t.Fatal("All() is empty")
	}
	first[0] = folders.Folder{UID: "clobbered", Title: "clobbered"}

	if folders.All()[0].UID == "clobbered" {
		t.Error("All() exposes the package's own slice; a caller can clobber it")
	}
}

func TestFolderValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		folder  folders.Folder
		wantErr string
	}{
		{name: "valid", folder: folders.Folder{UID: "ok-1", Title: "OK"}},
		{name: "valid single char", folder: folders.Folder{UID: "a", Title: "A"}},
		{name: "valid nested", folder: folders.Folder{UID: "child", Title: "Child", ParentUID: "parent"}},
		{name: "empty uid", folder: folders.Folder{Title: "T"}, wantErr: "uid is empty"},
		{name: "uid with space", folder: folders.Folder{UID: "not ok", Title: "T"}, wantErr: "DNS-1123"},
		{name: "uid with slash", folder: folders.Folder{UID: "a/b", Title: "T"}, wantErr: "DNS-1123"},
		{name: "uid over 40 chars", folder: folders.Folder{UID: strings.Repeat("a", 41), Title: "T"}, wantErr: "the CRD allows 40"},
		{name: "uid with underscore", folder: folders.Folder{UID: "a_b", Title: "T"}, wantErr: "DNS-1123"},
		{name: "uid uppercase", folder: folders.Folder{UID: "NotOk", Title: "T"}, wantErr: "DNS-1123"},
		{name: "uid trailing dash", folder: folders.Folder{UID: "trailing-", Title: "T"}, wantErr: "DNS-1123"},
		{name: "empty title", folder: folders.Folder{UID: "ok"}, wantErr: "title is empty"},
		{name: "blank title", folder: folders.Folder{UID: "ok", Title: "   "}, wantErr: "title is empty"},
		{name: "self parent", folder: folders.Folder{UID: "loop", Title: "T", ParentUID: "loop"}, wantErr: "is its own parent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.folder.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
