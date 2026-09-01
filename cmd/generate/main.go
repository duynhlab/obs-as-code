// Command generate renders every registered dashboard to JSON.
//
// The output is plain Grafana dashboard JSON — the same thing you would export
// from the UI or import into any Grafana, not a manifest for one particular
// cluster. Delivery is somebody else's problem by design: the release workflow
// publishes this tree as an OCI artifact, and a GrafanaDashboard resource in
// homelab points at a file inside it with spec.oci.
//
// Output is committed, so a pull request shows the dashboard change beside the
// Go change that caused it, and `make diff` fails when the two have drifted.
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

	// Imported for the registration side effect: this is what puts dashboards in
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
	outDir := fs.String("out", "generated", "directory to write dashboard JSON into")
	only := fs.String("profile", "all", `profile to render, or "all"`)
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

		v, err := renderProfile(p, *outDir, written, stdout)
		if err != nil {
			return err
		}
		violations = append(violations, v...)
	}

	// Rules run at generate time, not only in tests: a board that breaks one must
	// never reach the output tree, where a release would publish it.
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

	names := make([]string, 0, len(allProfiles()))
	for _, p := range allProfiles() {
		if p.Name == name {
			return []profile.Profile{p}, nil
		}
		names = append(names, p.Name)
	}

	return nil, fmt.Errorf("unknown profile %q; known profiles are %s", name, strings.Join(names, ", "))
}

func renderProfile(p profile.Profile, outDir string, written map[string]bool, stdout io.Writer) ([]check.Violation, error) {
	var violations []check.Violation

	for _, d := range registry.Published() {
		model, err := d.Model(p)
		if err != nil {
			return nil, err
		}

		violations = append(violations, check.Dashboard(d.UID, model)...)

		if err := writeFile(filepath.Join(outDir, p.Name, d.Filename()), model, written, stdout); err != nil {
			return nil, err
		}
	}

	return violations, nil
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
// Without it, deleting a board from the code leaves its JSON in the artifact and
// the GrafanaDashboard pointing at it keeps working — the board outlives the
// commit that removed it, which is the failure mode that makes people distrust
// generated output.
//
// Deletions go through os.Root, so a symlink planted in the output tree cannot
// steer this function at something outside it.
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
		if d.IsDir() || filepath.Ext(rel) != ".json" {
			// Anything this tool does not produce is left alone, so a README or
			// a .gitkeep can live in the tree.
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
