package catalog_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/duynhlab/obs-as-code/internal/catalog"
	"github.com/duynhlab/obs-as-code/internal/check"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

// profiles is every profile resources are rendered for. Adding one here is the
// whole cost of supporting a new target: no resource file changes.
func profiles() []profile.Profile {
	return []profile.Profile{profile.Cluster()}
}

// TestCatalogIsNotEmpty guards the failure mode every table-driven suite over a
// registry has: if nothing registers, every other test below iterates zero
// times and reports success.
func TestCatalogIsNotEmpty(t *testing.T) {
	t.Parallel()

	if len(registry.All()) == 0 {
		t.Fatal("no resources registered; every conformance test below would pass vacuously")
	}
}

func TestEveryResourceRenders(t *testing.T) {
	t.Parallel()

	for _, p := range profiles() {
		for _, r := range registry.All() {
			m := r.Describe()

			t.Run(p.Name+"/"+m.UID, func(t *testing.T) {
				t.Parallel()

				objs, err := r.Render(p)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}
				if len(objs) == 0 {
					t.Fatal("Render() produced no objects")
				}
			})
		}
	}
}

func TestEveryResourceObeysTheObjectRules(t *testing.T) {
	t.Parallel()

	for _, p := range profiles() {
		for _, r := range registry.All() {
			m := r.Describe()

			t.Run(p.Name+"/"+m.UID, func(t *testing.T) {
				t.Parallel()

				objs, err := r.Render(p)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}

				if v := check.Objects(m.UID, objs, p.Namespace, p.InstanceLabels); len(v) > 0 {
					t.Errorf("object rules:\n%s", check.Format(v))
				}
			})
		}
	}
}

func TestEveryDashboardObeysTheDashboardRules(t *testing.T) {
	t.Parallel()

	for _, p := range profiles() {
		for _, r := range registry.All() {
			d, isDashboard := r.(registry.Dashboard)
			if !isDashboard {
				continue
			}

			t.Run(p.Name+"/"+d.UID, func(t *testing.T) {
				t.Parallel()

				model, err := d.Model(p)
				if err != nil {
					t.Fatalf("Model() error = %v", err)
				}

				if v := check.Dashboard(d.UID, model); len(v) > 0 {
					t.Errorf("dashboard rules:\n%s", check.Format(v))
				}
			})
		}
	}
}

// TestDashboardGoldenFiles pins the rendered JSON. It does not assert the JSON
// is correct — the rules above do that. It asserts that nothing changed without
// someone looking, which is what makes a Foundation SDK upgrade reviewable
// instead of a leap of faith.
//
// Update with: make golden
func TestDashboardGoldenFiles(t *testing.T) {
	t.Parallel()

	for _, p := range profiles() {
		for _, r := range registry.All() {
			d, isDashboard := r.(registry.Dashboard)
			if !isDashboard {
				continue
			}

			t.Run(p.Name+"/"+d.UID, func(t *testing.T) {
				t.Parallel()

				got, err := d.Model(p)
				if err != nil {
					t.Fatalf("Model() error = %v", err)
				}

				path := filepath.Join("testdata", p.Name+"-"+d.UID+".golden.json")

				if *update {
					if err := os.WriteFile(path, got, 0o644); err != nil {
						t.Fatalf("write golden: %v", err)
					}
					t.Logf("updated %s", path)
					return
				}

				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden: %v — run `make golden` to create it", err)
				}

				if !bytes.Equal(got, want) {
					// Writing the actual output where the runner collects it
					// means a CI failure can be diffed without reproducing the
					// run locally.
					actual := filepath.Join(t.ArtifactDir(), filepath.Base(path))
					if err := os.WriteFile(actual, got, 0o644); err == nil {
						t.Logf("actual output written to %s", actual)
					}
					t.Errorf("rendered model differs from %s; if the change is intended run `make golden` and review the diff", path)
				}
			})
		}
	}
}

func TestRenderingIsDeterministic(t *testing.T) {
	t.Parallel()

	// Generated output is committed, so any instability shows up as a phantom
	// diff on an unrelated pull request rather than as a test failure here.
	for _, p := range profiles() {
		for _, r := range registry.All() {
			m := r.Describe()

			t.Run(p.Name+"/"+m.UID, func(t *testing.T) {
				t.Parallel()

				first, err := r.Render(p)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}
				second, err := r.Render(p)
				if err != nil {
					t.Fatalf("Render() error = %v", err)
				}
				if len(first) != len(second) {
					t.Fatalf("Render() produced %d then %d objects", len(first), len(second))
				}

				for i := range first {
					a, err := first[i].YAML()
					if err != nil {
						t.Fatalf("YAML() error = %v", err)
					}
					b, err := second[i].YAML()
					if err != nil {
						t.Fatalf("YAML() error = %v", err)
					}
					if !bytes.Equal(a, b) {
						t.Errorf("%s is not rendered deterministically", first[i].Path())
					}
				}
			})
		}
	}
}

func TestObjectPathsAreUnique(t *testing.T) {
	t.Parallel()

	for _, p := range profiles() {
		seen := make(map[string]string)

		for _, r := range registry.All() {
			m := r.Describe()
			objs, err := r.Render(p)
			if err != nil {
				t.Fatalf("%s: Render() error = %v", m.UID, err)
			}

			for _, obj := range objs {
				// Two resources writing the same path means one silently
				// overwrites the other on generate.
				if other, dup := seen[obj.Path()]; dup {
					t.Errorf("%s and %s both write %s", other, m.UID, obj.Path())
				}
				seen[obj.Path()] = m.UID
			}
		}
	}
}
