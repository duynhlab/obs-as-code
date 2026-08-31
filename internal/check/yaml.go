package check

import "sigs.k8s.io/yaml"

// yamlUnmarshal is a thin alias so the rules read as one story rather than
// alternating between two serialization libraries.
func yamlUnmarshal(data []byte, out any) error { return yaml.Unmarshal(data, out) }
