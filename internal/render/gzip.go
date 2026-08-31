package render

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"time"
)

// gzipJSON compresses b for GrafanaDashboard.spec.gzipJson, whose CRD documents
// it as "the model's JSON compressed with Gzip. Base64-encoded when in YAML."
// The base64 step is handled by encoding/json, which renders a []byte as a
// base64 string.
//
// The output must be byte-identical for identical input, because it lands in a
// committed file: a gzip header that varies per run would make every generate
// produce a diff and turn `make diff` into noise. The three header fields that
// could vary are pinned below rather than left to the standard library's
// defaults, which are correct today but are not part of its contract.
func gzipJSON(b []byte) ([]byte, error) {
	var buf bytes.Buffer

	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("render: new gzip writer: %w", err)
	}

	zw.Name = ""             // no source filename
	zw.Comment = ""          // no comment
	zw.ModTime = time.Time{} // zero, so no timestamp is written
	zw.OS = 255              // "unknown", per RFC 1952 — not the build host's OS

	if _, err := zw.Write(b); err != nil {
		return nil, fmt.Errorf("render: gzip write: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("render: gzip close: %w", err)
	}

	return buf.Bytes(), nil
}
