package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWritesV2DashboardsAndDeployableManifests(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")
	if err := run([]string{"-out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	raw := readJSON(t, filepath.Join(out, "cluster", "dashboards", "kubernetes-cluster-overview.json"))
	if got, want := raw["apiVersion"], "dashboard.grafana.app/v2"; got != want {
		t.Errorf("raw apiVersion = %v, want %q", got, want)
	}
	if got, want := raw["kind"], "Dashboard"; got != want {
		t.Errorf("raw kind = %v, want %q", got, want)
	}

	manifest := readJSON(t, filepath.Join(out, "cluster", "manifests", "kubernetes-cluster-overview.json"))
	if got, want := manifest["apiVersion"], "grafana.integreatly.org/v1beta1"; got != want {
		t.Errorf("manifest apiVersion = %v, want %q", got, want)
	}
	if got, want := manifest["kind"], "GrafanaManifest"; got != want {
		t.Errorf("manifest kind = %v, want %q", got, want)
	}

	if _, err := os.Stat(filepath.Join(out, "cluster", "manifests", "obs-as-code-example.json")); !os.IsNotExist(err) {
		t.Errorf("example deployable manifest exists; example must remain build/test-only: %v", err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return value
}

func TestRunWritesAndIsIdempotent(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")

	var first bytes.Buffer
	if err := run([]string{"-out", out}, &first); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if first.Len() == 0 {
		t.Fatal("run() wrote nothing; the output tree would be empty")
	}

	// A second run must be silent. If it is not, generated output churns and
	// `make diff` fails on pull requests that changed nothing.
	var second bytes.Buffer
	if err := run([]string{"-out", out}, &second); err != nil {
		t.Fatalf("second run() error = %v", err)
	}
	if second.Len() != 0 {
		t.Errorf("second run() is not idempotent:\n%s", second.String())
	}
}

func TestRunPrunesStaleFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")

	if err := run([]string{"-out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	// A board deleted from the code must not leave its JSON behind: it would stay
	// in the published artifact and any GrafanaDashboard pointing at it would
	// keep serving it, so the board would outlive the commit that removed it.
	stale := filepath.Join(out, "cluster", "dashboards", "deleted-board.json")
	if err := os.WriteFile(stale, []byte(`{"uid":"deleted-board"}`), 0o644); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}
	// A non-generated file must survive.
	keep := filepath.Join(out, "cluster", "README.md")
	if err := os.WriteFile(keep, []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("seed kept file: %v", err)
	}

	var log bytes.Buffer
	if err := run([]string{"-out", out}, &log); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale dashboard JSON survived: %v", err)
	}
	if !strings.Contains(log.String(), "removed") {
		t.Errorf("run() did not report the removal:\n%s", log.String())
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a non-generated file was deleted: %v", err)
	}
}

func TestRunRejectsAnUnknownProfile(t *testing.T) {
	err := run([]string{"-out", t.TempDir(), "-profile", "nope"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("run() = nil error, want an unknown-profile error")
	}
	if !strings.Contains(err.Error(), "known profiles are") {
		t.Errorf("run() = %v, want it to list the known profiles", err)
	}
}

func TestRunRendersASingleProfile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")

	if err := run([]string{"-out", out, "-profile", "cluster"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(out, "cluster", "dashboards")); err != nil {
		t.Errorf("cluster profile was not rendered: %v", err)
	}
}
