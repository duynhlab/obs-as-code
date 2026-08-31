// Command generate renders every registered dashboard and alert group into
// Grafana Operator resources.
//
// Output is committed to the repository, so a pull request shows the JSON change
// beside the Go change that caused it. `make diff` then fails when the two have
// drifted, which is the only thing keeping the committed tree honest.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/duynhlab/obs-as-code/internal/check"
	"github.com/duynhlab/obs-as-code/internal/profile"
	"github.com/duynhlab/obs-as-code/internal/registry"
	"github.com/duynhlab/obs-as-code/internal/render"

	// Imported for the registration side effect: this is what puts resources in
	// the registry.
	_ "github.com/duynhlab/obs-as-code/internal/catalog"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

// allProfiles is every profile that can be rendered. Supporting a new target
// means adding an entry here and nothing else.
func allProfiles() []profile.Profile {
	return []profile.Profile{profile.Cluster()}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	outDir := fs.String("out", "generated", "directory to write resources into")
	only := fs.String("profile", "all", `profile to render, or "all"`)
	modelDir := fs.String("models", "", "also write each dashboard model as a standalone JSON file here, for local inspection; not committed")
	if err := fs.Parse(args); err != nil {
		return err
	}

	profiles, err := selectProfiles(*only)
	if err != nil {
		return err
	}

	var violations []check.Violation
	written := make(map[string]bool)

	for _, p := range profiles {
		if err := p.Validate(); err != nil {
			return err
		}

		v, err := renderProfile(p, *outDir, *modelDir, written, stdout)
		if err != nil {
			return err
		}
		violations = append(violations, v...)
	}

	// Rules run at generate time, not only in tests: a resource that breaks one
	// must never reach the output tree, where Flux would happily apply it.
	if len(violations) > 0 {
		return fmt.Errorf("%d conformance violation(s):\n%s", len(violations), check.Format(violations))
	}

	removed, err := pruneStale(*outDir, written)
	if err != nil {
		return err
	}
	for _, path := range removed {
		fmt.Fprintf(stdout, "removed  %s\n", path)
	}

	return nil
}

func selectProfiles(name string) ([]profile.Profile, error) {
	if name == "all" {
		return allProfiles(), nil
	}

	for _, p := range allProfiles() {
		if p.Name == name {
			return []profile.Profile{p}, nil
		}
	}

	names := make([]string, 0, len(allProfiles()))
	for _, p := range allProfiles() {
		names = append(names, p.Name)
	}
	return nil, fmt.Errorf("unknown profile %q; known profiles are %s", name, strings.Join(names, ", "))
}

func renderProfile(p profile.Profile, outDir, modelDir string, written map[string]bool, stdout io.Writer) ([]check.Violation, error) {
	var violations []check.Violation
	var resourcePaths []string

	// Folders first, so a reader of the output tree sees the containers before
	// the things filed in them. The operator itself retries until a referenced
	// folder exists, so this is for humans, not for correctness.
	for _, f := range registry.Folders() {
		obj, err := render.Folder(p, f)
		if err != nil {
			return nil, err
		}
		path, err := writeObject(outDir, p.Name, obj, written, stdout)
		if err != nil {
			return nil, err
		}
		resourcePaths = append(resourcePaths, path)
	}

	for _, r := range registry.Published() {
		m := r.Describe()

		objs, err := r.Render(p)
		if err != nil {
			return nil, err
		}

		violations = append(violations, check.Objects(m.UID, objs, p.Namespace, p.InstanceLabels)...)

		for _, obj := range objs {
			path, err := writeObject(outDir, p.Name, obj, written, stdout)
			if err != nil {
				return nil, err
			}
			resourcePaths = append(resourcePaths, path)
		}

		// The resource embeds the model as readable JSON, so there is nothing to
		// write twice. A standalone copy is produced only when asked for, since
		// Grafana's file provisioning (make preview) wants loose JSON files.
		if d, isDashboard := r.(registry.Dashboard); isDashboard {
			model, err := d.Model(p)
			if err != nil {
				return nil, err
			}
			violations = append(violations, check.Dashboard(d.UID, model)...)

			if modelDir != "" {
				path := filepath.Join(modelDir, p.Name, d.UID+".json")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return nil, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
				}
				if err := os.WriteFile(path, model, 0o644); err != nil {
					return nil, fmt.Errorf("write %s: %w", path, err)
				}
			}
		}
	}

	if err := writeKustomization(outDir, p.Name, resourcePaths, written, stdout); err != nil {
		return nil, err
	}

	return violations, nil
}

func writeObject(outDir, profileName string, obj render.Object, written map[string]bool, stdout io.Writer) (string, error) {
	body, err := obj.YAML()
	if err != nil {
		return "", err
	}

	rel := obj.Path()
	if err := writeFile(filepath.Join(outDir, profileName, rel), body, written, stdout); err != nil {
		return "", err
	}
	return rel, nil
}

// writeKustomization lists only the resource files, so the models directory is
// never applied to a cluster.
func writeKustomization(outDir, profileName string, resources []string, written map[string]bool, stdout io.Writer) error {
	slices.Sort(resources)

	var b strings.Builder
	b.WriteString("# Generated by obs-as-code — DO NOT EDIT.\n")
	b.WriteString("# Regenerate with: make generate\n")
	b.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\n")
	b.WriteString("kind: Kustomization\n")
	b.WriteString("resources:\n")
	for _, r := range resources {
		fmt.Fprintf(&b, "  - %s\n", r)
	}

	return writeFile(filepath.Join(outDir, profileName, "kustomization.yaml"), []byte(b.String()), written, stdout)
}

func writeFile(path string, body []byte, written map[string]bool, stdout io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	existing, err := os.ReadFile(path)
	switch {
	case err == nil && string(existing) == string(body):
		written[filepath.Clean(path)] = true
		return nil
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read %s: %w", path, err)
	}

	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	written[filepath.Clean(path)] = true
	fmt.Fprintf(stdout, "wrote    %s\n", path)

	return nil
}

// pruneStale deletes files under outDir that this run did not write.
//
// Without it, deleting a dashboard from the code leaves its resource in the tree
// and Flux keeps applying it forever — the board outlives the commit that
// removed it, which is the failure mode that makes people distrust generated
// output.
//
// Deletions go through os.Root, so a symlink planted in the output tree cannot
// steer this function at something outside it. That is cheap insurance for the
// one function here whose job is removing files.
func pruneStale(outDir string, written map[string]bool) ([]string, error) {
	root, err := os.OpenRoot(outDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", outDir, err)
	}
	defer func() { _ = root.Close() }()

	var removed []string

	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Anything this tool does not produce is left alone, so a README or a
		// .gitkeep can live in the tree.
		switch filepath.Ext(rel) {
		case ".yaml", ".json":
		default:
			return nil
		}

		if written[filepath.Clean(filepath.Join(outDir, rel))] {
			return nil
		}

		if err := root.Remove(rel); err != nil {
			return fmt.Errorf("remove stale %s: %w", rel, err)
		}
		removed = append(removed, filepath.Join(outDir, rel))
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(removed)
	return removed, nil
}
