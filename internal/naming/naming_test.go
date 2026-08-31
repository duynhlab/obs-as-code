package naming_test

import (
	"strings"
	"testing"

	"github.com/duynhlab/obs-as-code/internal/naming"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		wantErr string
	}{
		{name: "simple", id: "envoy-edge"},
		{name: "single char", id: "a"},
		{name: "digits", id: "pg-17"},
		{name: "at max length", id: strings.Repeat("a", naming.MaxLen)},
		{name: "empty", id: "", wantErr: "uid is empty"},
		{name: "over max length", id: strings.Repeat("a", naming.MaxLen+1), wantErr: "the CRD allows 40"},

		// Each of these is accepted by the CRD's own pattern and rejected by
		// the Kubernetes API server. They are the whole reason this package
		// exists rather than reusing the CRD's regex.
		{name: "underscore", id: "envoy_edge", wantErr: "DNS-1123"},
		{name: "uppercase", id: "EnvoyEdge", wantErr: "DNS-1123"},

		{name: "leading dash", id: "-edge", wantErr: "DNS-1123"},
		{name: "trailing dash", id: "edge-", wantErr: "DNS-1123"},
		{name: "dot", id: "envoy.edge", wantErr: "DNS-1123"},
		{name: "slash", id: "envoy/edge", wantErr: "DNS-1123"},
		{name: "space", id: "envoy edge", wantErr: "DNS-1123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := naming.Validate("dashboard", tt.id)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%q) = %v, want nil", tt.id, err)
				}
				if !naming.IsValid(tt.id) {
					t.Errorf("IsValid(%q) = false, want true", tt.id)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%q) = nil, want an error containing %q", tt.id, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate(%q) = %v, want it to contain %q", tt.id, err, tt.wantErr)
			}
			if naming.IsValid(tt.id) {
				t.Errorf("IsValid(%q) = true, want false", tt.id)
			}
		})
	}
}

func TestValidateNamesTheKind(t *testing.T) {
	t.Parallel()

	// The error has to say what to go fix; "uid must be a DNS-1123 label" with
	// no subject sends the reader hunting.
	err := naming.Validate("alert rule group", "Bad_Name")
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	if !strings.Contains(err.Error(), "alert rule group") {
		t.Errorf("Validate() = %v, want it to name the kind", err)
	}
}
