package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

	manifest := readJSON(t, filepath.Join(out, "cluster", "manifests", "dashboards", "kubernetes-cluster-overview.json"))
	if got, want := manifest["apiVersion"], "grafana.integreatly.org/v1beta1"; got != want {
		t.Errorf("manifest apiVersion = %v, want %q", got, want)
	}
	if got, want := manifest["kind"], "GrafanaManifest"; got != want {
		t.Errorf("manifest kind = %v, want %q", got, want)
	}

	if _, err := os.Stat(filepath.Join(out, "cluster", "manifests", "dashboards", "obs-as-code-example.json")); !os.IsNotExist(err) {
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

// TestGeneratedBoardsResolveTheirDatasource walks every board this generator
// actually writes and asserts the datasource wiring end to end.
//
// It exists because the unit tests around profile and check both passed while
// every V2 board rendered empty in Grafana: the datasource variable's
// current.value carried the datasource's display name, and ${ds} therefore
// expanded to a string no lookup could resolve. Per-package tests could not see
// that, because neither of them looks at the file that ships.
//
// This is the only test that catches the defect no matter where it is
// reintroduced — a profile change, a builder change, or a new board that
// declares its own variable.
//
// It cannot prove the uid exists in a real Grafana; that is a cluster fact.
// Verify that separately with:
//
//	kubectl -n monitoring exec deploy/grafana-deployment -- \
//	  wget -qO- http://localhost:3000/api/datasources/uid/<uid>
func TestGeneratedBoardsResolveTheirDatasource(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")
	if err := run([]string{"-out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	boards, err := filepath.Glob(filepath.Join(out, "*", "dashboards", "*.json"))
	if err != nil {
		t.Fatalf("glob boards: %v", err)
	}
	// Guard against a glob that silently matches nothing, which would make
	// every assertion below pass vacuously.
	if len(boards) == 0 {
		t.Fatal("no generated boards found; the assertions below would pass vacuously")
	}

	uidShape := regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

	for _, path := range boards {
		t.Run(filepath.Base(path), func(t *testing.T) {
			board := readJSON(t, path)

			spec, ok := board["spec"].(map[string]any)
			if !ok {
				t.Fatalf("spec has type %T", board["spec"])
			}
			variables, ok := spec["variables"].([]any)
			if !ok {
				t.Fatalf("spec.variables has type %T", spec["variables"])
			}

			declared := make(map[string]bool)
			var sawDatasourceVar bool

			for _, raw := range variables {
				v, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("variable has type %T", raw)
				}
				vSpec, ok := v["spec"].(map[string]any)
				if !ok {
					t.Fatalf("variable spec has type %T", v["spec"])
				}
				name, _ := vSpec["name"].(string)
				if name != "" {
					declared["${"+name+"}"] = true
				}
				if v["kind"] != "DatasourceVariable" {
					continue
				}
				sawDatasourceVar = true

				current, ok := vSpec["current"].(map[string]any)
				if !ok {
					t.Fatalf("datasource variable %q has no current; Grafana would pick a datasource for us", name)
				}
				value, ok := current["value"].(string)
				if !ok {
					t.Fatalf("datasource variable %q has current.value of type %T, want one uid string", name, current["value"])
				}
				if !uidShape.MatchString(value) {
					t.Errorf("datasource variable %q has current.value %q, want a uid matching %s (a display name cannot resolve)", name, value, uidShape)
				}
				if text, ok := current["text"].(string); ok && text == value {
					t.Errorf("datasource variable %q has text == value == %q; text is a label and value is a uid, so they must differ", name, value)
				}
			}

			if !sawDatasourceVar {
				t.Error("board declares no DatasourceVariable, so every panel would fall back to Grafana's default datasource")
			}

			// Every panel must point at a variable this board declares. check.go
			// asserts this on the in-memory model; repeating it on the file that
			// ships proves the generate path cannot route around the checker.
			assertPanelDatasourcesAreDeclared(t, spec, declared)
		})
	}
}

func assertPanelDatasourcesAreDeclared(t *testing.T, spec map[string]any, declared map[string]bool) {
	t.Helper()

	elements, ok := spec["elements"].(map[string]any)
	if !ok {
		t.Fatalf("spec.elements has type %T", spec["elements"])
	}
	if len(elements) == 0 {
		t.Fatal("board has no elements; the assertions below would pass vacuously")
	}

	for key, raw := range elements {
		element, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("element %q has type %T", key, raw)
		}
		eSpec, _ := element["spec"].(map[string]any)
		data, _ := eSpec["data"].(map[string]any)
		dataSpec, _ := data["spec"].(map[string]any)
		queries, _ := dataSpec["queries"].([]any)

		for _, rawQuery := range queries {
			query, ok := rawQuery.(map[string]any)
			if !ok {
				t.Fatalf("element %q has a query of type %T", key, rawQuery)
			}
			qSpec, _ := query["spec"].(map[string]any)
			inner, _ := qSpec["query"].(map[string]any)
			ds, ok := inner["datasource"].(map[string]any)
			if !ok {
				t.Errorf("element %q has a query with no datasource", key)
				continue
			}
			name, _ := ds["name"].(string)
			if !declared[name] {
				t.Errorf("element %q queries datasource %q, which this board does not declare as a variable", key, name)
			}
		}
	}
}

// TestGeneratedBoardsSelectAVariableValue guards the second defect that shipped
// empty boards, and it is a different failure from the datasource one above.
//
// Dashboard V2's QueryVariableSpec.Current is a value field with no omitempty,
// so it is serialised whether or not anyone set it, and an unset one marshals
// as {"text":"","value":""}. Grafana honours that as a real selection of the
// empty string, so $namespace expanded to "" and every panel filtering on it
// matched nothing. Classic dashboards emitted current: null instead, which
// Grafana treats as "no selection yet" and resolves from the options — which is
// why the v0.3.0 boards kept working while the V2 ones went blank.
//
// Measured on the cluster: namespace=~"" returned 0 data points where
// namespace=~".*" returned 28.
//
// The assertion is deliberately about emptiness rather than a specific value:
// what breaks a board is shipping a selection nobody made, not which value was
// chosen.
func TestGeneratedBoardsSelectAVariableValue(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")
	if err := run([]string{"-out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	boards, err := filepath.Glob(filepath.Join(out, "*", "dashboards", "*.json"))
	if err != nil {
		t.Fatalf("glob boards: %v", err)
	}
	if len(boards) == 0 {
		t.Fatal("no generated boards found; the assertions below would pass vacuously")
	}

	var checked int
	for _, path := range boards {
		t.Run(filepath.Base(path), func(t *testing.T) {
			board := readJSON(t, path)
			spec, ok := board["spec"].(map[string]any)
			if !ok {
				t.Fatalf("spec has type %T", board["spec"])
			}
			variables, ok := spec["variables"].([]any)
			if !ok {
				t.Fatalf("spec.variables has type %T", spec["variables"])
			}

			for _, raw := range variables {
				v, _ := raw.(map[string]any)
				if v["kind"] != "QueryVariable" {
					continue
				}
				vSpec, _ := v["spec"].(map[string]any)
				name, _ := vSpec["name"].(string)
				checked++

				current, ok := vSpec["current"].(map[string]any)
				if !ok {
					t.Errorf("query variable %q has no current object", name)
					continue
				}
				value, _ := current["value"].(string)
				if value == "" {
					t.Errorf("query variable %q ships current.value %q; an empty selection makes every panel filtering on it match nothing", name, value)
				}
				if text, _ := current["text"].(string); text == "" {
					t.Errorf("query variable %q ships an empty current.text, so the picker renders blank", name)
				}

				// The SDK defaults allowCustomValue to true and nobody set it.
				// On a namespace or workload picker that lets a reader type a
				// value that does not exist, and the panels then render empty
				// with nothing saying why — the same silent-emptiness this test
				// exists to prevent, arrived at by hand instead of by default.
				if allow, ok := vSpec["allowCustomValue"].(bool); !ok || allow {
					t.Errorf("query variable %q has allowCustomValue = %v, want false; a typed-in value that does not exist renders an empty panel with no error", name, vSpec["allowCustomValue"])
				}
			}
		})
	}

	// Every board here declares at least one query variable, so a zero count
	// means the kind filter stopped matching and the loop above proved nothing.
	if checked == 0 {
		t.Fatal("no QueryVariable was inspected; the kind filter no longer matches the generated shape")
	}
}

// TestRunSplitsFoldersFromDashboards pins the artifact layout that lets Flux
// order the two apply waves.
//
// The Grafana operator applies each GrafanaManifest independently with no
// ordering guarantee, and a dashboard naming a folder that does not exist yet
// fails with "folders.folder.grafana.app ... not found". Measured on a cold
// bring-up: the folder succeeded, both dashboards failed, and the wave sat
// red for six minutes until the operator's next resync — its resyncPeriod of
// 10m being longer than the wave's 5m timeout.
//
// Ordering inside one wave guarantees nothing; only dependsOn between two
// waves does, and two waves need two paths. That is what this asserts.
func TestRunSplitsFoldersFromDashboards(t *testing.T) {
	out := filepath.Join(t.TempDir(), "generated")
	if err := run([]string{"-out", out}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	manifests := filepath.Join(out, "cluster", "manifests")
	folders := filepath.Join(manifests, "folders")
	dashboards := filepath.Join(manifests, "dashboards")

	// Each directory is a kustomize root of its own, so each needs its own
	// resource list. A single shared Kustomization would collapse the split.
	for _, dir := range []string{folders, dashboards} {
		if _, err := os.Stat(filepath.Join(dir, "Kustomization")); err != nil {
			t.Errorf("%s has no Kustomization, so Flux cannot apply it as a wave: %v", dir, err)
		}
	}

	folder := readJSON(t, filepath.Join(folders, "platform-infrastructure.json"))
	if got := folder["kind"]; got != "GrafanaManifest" {
		t.Errorf("folder manifest kind = %v, want GrafanaManifest", got)
	}
	if _, err := os.Stat(filepath.Join(dashboards, "platform-infrastructure.json")); !os.IsNotExist(err) {
		t.Errorf("the folder is also in the dashboards directory, which puts it back in the same wave: %v", err)
	}

	for _, name := range []string{"kubernetes-cluster-overview.json", "kubernetes-workloads.json"} {
		if _, err := os.Stat(filepath.Join(dashboards, name)); err != nil {
			t.Errorf("%s is not in the dashboards directory: %v", name, err)
		}
	}

	// Nothing may remain directly under manifests/: a leftover there would be
	// applied by neither wave, or by both.
	entries, err := os.ReadDir(manifests)
	if err != nil {
		t.Fatalf("read %s: %v", manifests, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("%s is directly under manifests/ and belongs to no wave", e.Name())
		}
	}
}
