package render

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestGzipJSONRoundTrips(t *testing.T) {
	t.Parallel()

	want := []byte(`{"title":"Example","panels":[]}`)

	compressed, err := gzipJSON(want)
	if err != nil {
		t.Fatalf("gzipJSON() error = %v", err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := zr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("round trip = %q, want %q", got, want)
	}
}

func TestGzipJSONIsDeterministic(t *testing.T) {
	t.Parallel()

	// The output is committed to git, so a header field that varies per run
	// (a timestamp, the build host's OS) would make every `make generate`
	// produce a diff and destroy the value of `make diff` as a CI gate.
	in := []byte(strings.Repeat(`{"panel":"x"}`, 100))

	first, err := gzipJSON(in)
	if err != nil {
		t.Fatalf("gzipJSON() error = %v", err)
	}
	second, err := gzipJSON(in)
	if err != nil {
		t.Fatalf("gzipJSON() error = %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Error("gzipJSON() is not deterministic; two calls on identical input differ")
	}
}

func TestGzipJSONHeaderCarriesNoBuildMetadata(t *testing.T) {
	t.Parallel()

	compressed, err := gzipJSON([]byte(`{}`))
	if err != nil {
		t.Fatalf("gzipJSON() error = %v", err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer func() { _ = zr.Close() }()

	if !zr.ModTime.IsZero() {
		t.Errorf("ModTime = %v, want zero", zr.ModTime)
	}
	if zr.Name != "" {
		t.Errorf("Name = %q, want empty", zr.Name)
	}
	if zr.OS != 255 {
		t.Errorf("OS = %d, want 255 (unknown)", zr.OS)
	}
}

func TestGzipJSONBase64EncodesAsBytes(t *testing.T) {
	t.Parallel()

	// The CRD field is []byte, and the base64 step that the CRD documents is
	// encoding/json's, not ours. Proving that here means the emitter never has
	// to base64 anything by hand.
	compressed, err := gzipJSON([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("gzipJSON() error = %v", err)
	}

	encoded, err := json.Marshal(struct {
		GzipJSON []byte `json:"gzipJson"`
	}{GzipJSON: compressed})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded struct {
		GzipJSON []byte `json:"gzipJson"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !bytes.Equal(decoded.GzipJSON, compressed) {
		t.Error("gzipJson did not survive a JSON round trip as base64")
	}
	if bytes.Contains(encoded, []byte{0x1f, 0x8b}) {
		t.Error("gzip magic bytes appear raw in the JSON; expected base64")
	}
}
