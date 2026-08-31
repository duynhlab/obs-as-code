// Package folders is the single declaration of every Grafana folder this repo
// owns.
//
// The cluster currently has no GrafanaFolder resources at all: folders are
// implicit strings repeated in each dashboard resource and again in the
// local-stack provisioning, and the two have already drifted apart once.
// Declaring them once here means a board references a folder it cannot
// misspell, and the same declaration generates the folder resource.
package folders

import (
	"fmt"
	"slices"
	"strings"

	"github.com/duynhlab/obs-as-code/internal/naming"
)

// Folder is a Grafana folder. The zero Folder is not usable; reference one of
// the package-level variables instead.
type Folder struct {
	// UID is the folder's stable identifier. The GrafanaFolder CRD marks
	// spec.uid immutable, so changing this value means deleting and recreating
	// the folder — and every dashboard filed under it.
	UID string

	// Title is the display name shown in Grafana's folder list.
	Title string

	// ParentUID nests this folder under another. Empty means top level.
	ParentUID string
}

// The folders this repo owns. Titles match what the cluster already shows so
// generated boards land beside the hand-written ones rather than beside a
// near-duplicate folder.
var (
	// Examples holds boards that exist to exercise the generator itself.
	Examples = Folder{UID: "obs-as-code-examples", Title: "Examples / obs-as-code"}

	// GoldenSignals holds per-service request rate, errors and latency.
	GoldenSignals = Folder{UID: "microservices-golden-signals", Title: "Microservices / Golden Signals"}

	// Business holds business and product KPIs. RFC-0017 requires these stay
	// out of the golden-signals boards.
	Business = Folder{UID: "business-and-product", Title: "Business & Product"}

	// APIGateway holds Envoy Gateway data-plane and control-plane boards.
	APIGateway = Folder{UID: "api-gateway", Title: "API Gateway"}

	// Databases holds CloudNativePG, PgBouncer and PgDog boards.
	Databases = Folder{UID: "databases", Title: "Databases"}

	// Kubernetes holds cluster, node and workload boards.
	Kubernetes = Folder{UID: "kubernetes", Title: "Kubernetes"}
)

// all is the registration order; All returns a copy so a caller cannot reorder
// or extend the package's own list.
var all = []Folder{Examples, GoldenSignals, Business, APIGateway, Databases, Kubernetes}

// All returns every declared folder, in declaration order.
func All() []Folder { return slices.Clone(all) }

// Validate reports whether f can be rendered into a GrafanaFolder resource.
func (f Folder) Validate() error {
	if err := naming.Validate("folder", f.UID); err != nil {
		return err
	}

	switch {
	case strings.TrimSpace(f.Title) == "":
		return fmt.Errorf("folder %q: title is empty", f.UID)
	case f.ParentUID != "" && f.ParentUID == f.UID:
		return fmt.Errorf("folder %q: is its own parent", f.UID)
	default:
		return nil
	}
}
